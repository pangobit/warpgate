// Package deploy handles application deployment to remote nodes.
package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/compose"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/release"
	"github.com/pangobit/warpgate/pkg/secrets"
	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/sirupsen/logrus"
)

const remoteAppsDir = "/opt/warpgate/apps"

// Deployer orchestrates application deployments to remote nodes.
type Deployer struct {
	// Repo is the loaded repo configuration.
	Repo *config.RepoConfig
	// SSHKey is the path to the SSH private key.
	SSHKey string
	// TailscaleSSH uses Tailscale SSH instead of key-based auth.
	TailscaleSSH bool
	// DryRun prints actions without executing them.
	DryRun bool
	// User is the SSH username. Defaults to the current OS user.
	User string
	// GitHubToken is an optional token for authenticating GitHub API requests
	// when fetching compose files from private repositories.
	GitHubToken string

	log *logrus.Logger
}

// NewDeployer creates a new deployer instance.
func NewDeployer(repo *config.RepoConfig, sshKey string) *Deployer {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		ForceColors: true,
	})

	return &Deployer{
		Repo:   repo,
		SSHKey: sshKey,
		log:    log,
	}
}

// Deploy deploys an app to its target nodes with zero-downtime blue/green swaps.
func (d *Deployer) Deploy(appName, version string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	if version != "" {
		return fmt.Errorf("positional version deploys are no longer supported; create or deploy a release instead")
	}

	appDir, composeContent, err := d.loadComposeContent(app)
	if err != nil {
		return err
	}

	manifest := release.Build(app, composeContent, time.Now())
	return d.deployManifest(app, manifest, appDir, composeContent)
}

// DeployRelease deploys a persisted release manifest by ID or "latest".
func (d *Deployer) DeployRelease(appName, releaseID string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	manifest, err := release.Load(d.Repo.AppReleasesDir(appName), releaseID)
	if err != nil {
		return err
	}
	if manifest.App != app.Name {
		return fmt.Errorf("release %s belongs to app %s, not %s", manifest.ID, manifest.App, app.Name)
	}

	appDir, composeContent, err := d.loadComposeContent(app)
	if err != nil {
		return err
	}

	return d.deployManifest(app, manifest, appDir, composeContent)
}

// CreateRelease snapshots the current deploy inputs and writes a release manifest.
func (d *Deployer) CreateRelease(appName string) (*release.Manifest, error) {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return nil, fmt.Errorf("app '%s' not found", appName)
	}

	_, composeContent, err := d.loadComposeContent(app)
	if err != nil {
		return nil, err
	}

	manifest := release.Build(app, composeContent, time.Now())
	if err := release.Save(d.Repo.AppReleasesDir(appName), manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (d *Deployer) deployManifest(app *config.AppConfig, manifest *release.Manifest, appDir string, composeContent []byte) error {
	internalHosts := d.Repo.InternalHosts()

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	d.log.Infof("Deploying %s release %s to nodes: %v", app.Name, manifest.ID, targetNodes)

	if d.DryRun {
		if app.Strategy == config.StrategyRecreate {
			d.log.Info("DRY RUN — recreate deploy (brief downtime)")
		} else {
			d.log.Info("DRY RUN — zero-downtime deploy via blue/green swap")
		}
		fmt.Println()
		dryRunIP := "<nodePrivateIP>"
		for _, nodeID := range targetNodes {
			if n := d.Repo.Cluster.GetNode(nodeID); n != nil && n.PrivateIP != "" {
				dryRunIP = n.PrivateIP
				break
			}
		}
		envFiles := releaseEnvFileMap(manifest, d.Repo.Cluster.Secrets.Server)
		override, err := compose.GenerateReleaseOverrideWithEnvFiles(app, manifest, envFiles, &d.Repo.Cluster.Networking, internalHosts, dryRunIP, string(composeContent))
		if err != nil {
			return fmt.Errorf("failed to generate override: %w", err)
		}
		fmt.Println("Override that will be applied:")
		fmt.Println(override)
		fmt.Printf("Targets: %v\n", targetNodes)
		if app.Source != nil {
			fmt.Printf("Compose source: %s@%s (path: %s)\n", app.Source.Repo, app.ComposeRef, app.Source.ComposePath)
		}
		services := manifest.Services
		for _, serviceName := range sortedManifestServiceNames(services) {
			service := services[serviceName]
			if service.SecretsPrefix != "" && d.Repo.Cluster.Secrets.Server != "" {
				fmt.Printf("Secrets: %s fetch from %s (prefix: %s) → %s\n", serviceName, d.Repo.Cluster.Secrets.Server, service.SecretsPrefix, serviceEnvFile(serviceName))
			}
			if len(service.Environment) > 0 {
				fmt.Printf("Environment: %s has %d variable(s) from release → %s\n", serviceName, len(service.Environment), serviceEnvFile(serviceName))
			}
		}
		if app.Strategy == config.StrategyRecreate {
			fmt.Printf("Each node: pull -> stop old -> start new -> health check\n")
		} else {
			fmt.Printf("Each node: pull -> start new -> health check -> stop old\n")
		}
		return nil
	}

	var deployed []string
	for i, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			d.log.Warnf("Node '%s' not found in config, skipping", nodeID)
			continue
		}

		d.log.Infof("[%d/%d] Deploying to node %s...", i+1, len(targetNodes), nodeID)

		envFiles := releaseEnvFileMap(manifest, d.Repo.Cluster.Secrets.Server)
		override, err := compose.GenerateReleaseOverrideWithEnvFiles(app, manifest, envFiles, &d.Repo.Cluster.Networking, internalHosts, node.PrivateIP, string(composeContent))
		if err != nil {
			return fmt.Errorf("failed to generate override for node %s: %w", nodeID, err)
		}

		if err := d.deployToNode(app, manifest, node, appDir, composeContent, override); err != nil {
			d.log.Warnf("Deploy to node %s failed: %v", nodeID, err)
			if len(deployed) > 0 {
				d.log.Warnf("Nodes already updated: %v", deployed)
			}
			return fmt.Errorf("rolling deploy stopped at node %s (%d/%d): %w", nodeID, i+1, len(targetNodes), err)
		}

		deployed = append(deployed, nodeID)
	}

	for _, serviceName := range sortedReleaseServiceConfigKeys(app.Release.Services) {
		service := app.Release.Services[serviceName]
		if service.EffectiveExpose().Internal != nil {
			if err := d.updateReleaseServiceInternalRoutes(app, serviceName, service); err != nil {
				d.log.Warnf("Failed to update internal routes for service %s: %v", serviceName, err)
			}
		}
	}

	if err := d.updateInternalProxy(app); err != nil {
		d.log.Warnf("Failed to update internal proxy: %v", err)
	}

	d.log.Infof("Deploy complete: %s release %s across %d node(s)", app.Name, manifest.ID, len(deployed))
	return nil
}

func (d *Deployer) loadComposeContent(app *config.AppConfig) (string, []byte, error) {
	appDir := d.Repo.AppDir(app.Name)
	composePath := d.Repo.AppComposePath(app.Name)

	if app.Source != nil {
		d.log.Infof("Fetching compose from %s@%s...", app.Source.Repo, app.ComposeRef)
		composeContent, err := FetchComposeFromSource(app.Source, app.ComposeRef, d.GitHubToken)
		if err != nil {
			return "", nil, fmt.Errorf("failed to fetch remote compose: %w", err)
		}
		return "", composeContent, nil
	}

	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("compose.yml not found for app '%s' at %s", app.Name, composePath)
	}
	composeContent, err := os.ReadFile(composePath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read compose.yml: %w", err)
	}
	return appDir, composeContent, nil
}

func (d *Deployer) deployToNode(app *config.AppConfig, manifest *release.Manifest, node *config.NodeConfig, appDir string, composeContent []byte, override string) error {
	client, err := d.connect(node)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir := remoteAppsDir + "/" + app.Name

	if _, _, err := client.RunCommand("mkdir -p " + remoteDir); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	if err := acquireLock(client, remoteDir, d.log); err != nil {
		return fmt.Errorf("node %s: %w", node.ID, err)
	}
	defer releaseLock(client, remoteDir, d.log)

	currentState := d.readState(client, remoteDir)
	restorePlan := recreateRestorePlan{}
	if app.Strategy == config.StrategyRecreate {
		var err error
		restorePlan, err = d.prepareRecreateRestore(client, remoteDir, app, currentState)
		if err != nil {
			return err
		}
	}

	if err := d.uploadDeploymentFiles(client, remoteDir, appDir, composeContent, override); err != nil {
		return err
	}

	hasEnvFile, err := d.writeReleaseEnvFile(client, remoteDir, manifest)
	if err != nil {
		return err
	}

	if err := d.loginRegistry(client); err != nil {
		return err
	}

	composeFiles := "-f compose.yml -f docker-compose.override.yml"
	result, err := d.deployWithStrategy(client, remoteDir, app, currentState, composeFiles, hasEnvFile, WaitForHealthy)
	if err != nil {
		return d.recoverFailedRecreateDeployment(client, remoteDir, app, restorePlan, composeFiles, hasEnvFile, err, WaitForHealthy)
	}

	if hasEnvFile {
		if err := removeReleaseEnvFiles(client, remoteDir, manifest); err != nil {
			return err
		}
	}

	d.saveDeployState(client, remoteDir, app, manifest, currentState, result.ActiveSlot)

	if result.ActiveSlot == "" {
		d.log.Infof("Node %s: %s release %s deployed", node.ID, app.Name, manifest.ID)
	} else {
		d.log.Infof("Node %s: %s slot active, %s release %s deployed", node.ID, result.ActiveSlot, app.Name, manifest.ID)
	}
	return nil
}

// Rollback re-deploys the previous version of an app.
func (d *Deployer) Rollback(appName string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no target nodes for %s", appName)
	}

	node := d.Repo.Cluster.GetNode(targetNodes[0])
	if node == nil {
		return fmt.Errorf("node %s not found", targetNodes[0])
	}

	client, err := d.connect(node)
	if err != nil {
		return err
	}
	remoteDir := remoteAppsDir + "/" + appName
	state := d.readState(client, remoteDir)
	client.Close()

	if state.PreviousRelease != "" {
		d.log.Infof("Rolling back %s from release %s to %s", appName, state.CurrentRelease, state.PreviousRelease)
		return d.DeployRelease(appName, state.PreviousRelease)
	}

	if state.PreviousVersion == "" {
		return fmt.Errorf("no previous release found for %s", appName)
	}
	d.log.Infof("Rolling back %s from %s to %s", appName, state.CurrentVersion, state.PreviousVersion)
	return d.Deploy(appName, state.PreviousVersion)
}

// DeployAll deploys every discovered app in order, continuing past failures.
func (d *Deployer) DeployAll() error {
	if len(d.Repo.Apps) == 0 {
		return fmt.Errorf("no apps found")
	}

	var failed []string
	for _, app := range d.Repo.Apps {
		d.log.Infof("--- Deploying %s ---", app.Name)
		if err := d.Deploy(app.Name, ""); err != nil {
			d.log.Warnf("Deploy failed for %s: %v", app.Name, err)
			failed = append(failed, app.Name)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("deploy failed for %d app(s): %s", len(failed), strings.Join(failed, ", "))
	}

	d.log.Infof("All %d app(s) deployed successfully", len(d.Repo.Apps))
	return nil
}

// RemoveAll removes every discovered app, continuing past failures.
func (d *Deployer) RemoveAll(nodeIDs []string) error {
	if len(d.Repo.Apps) == 0 {
		return fmt.Errorf("no apps found")
	}

	var failed []string
	for _, app := range d.Repo.Apps {
		d.log.Infof("--- Removing %s ---", app.Name)
		if err := d.Remove(app.Name, nodeIDs); err != nil {
			d.log.Warnf("Remove failed for %s: %v", app.Name, err)
			failed = append(failed, app.Name)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("remove failed for %d app(s): %s", len(failed), strings.Join(failed, ", "))
	}

	d.log.Infof("All %d app(s) removed successfully", len(d.Repo.Apps))
	return nil
}

// Remove stops and removes an app from all target nodes.
func (d *Deployer) Remove(appName string, nodeIDs []string) error {
	if len(nodeIDs) == 0 {
		app := d.Repo.GetApp(appName)
		if app == nil {
			for _, node := range d.Repo.Cluster.Nodes {
				nodeIDs = append(nodeIDs, node.ID)
			}
			d.log.Warnf("App '%s' not found in config, scanning all nodes", appName)
		} else {
			nodeIDs = app.GetTargetNodes(d.Repo.Cluster.Nodes)
		}
	}

	if len(nodeIDs) == 0 {
		return fmt.Errorf("no nodes to remove %s from", appName)
	}

	var removed []string
	for _, nodeID := range nodeIDs {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			d.log.Warnf("Node '%s' not found in config, skipping", nodeID)
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s: %v", nodeID, err)
			continue
		}

		remoteDir := remoteAppsDir + "/" + appName

		if err := acquireLock(client, remoteDir, d.log); err != nil {
			client.Close()
			d.log.Warnf("Node %s: %v", nodeID, err)
			continue
		}

		state := d.readState(client, remoteDir)
		composeFiles := "-f compose.yml -f docker-compose.override.yml"

		for _, projectFlag := range allDeploymentProjectFlags(appName) {
			stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down 2>/dev/null || true", remoteDir, projectFlag, composeFiles)
			if _, _, err := client.RunCommand(stopCmd); err != nil {
				d.log.Warnf("Failed to stop deployment %s on %s: %v", projectFlag, nodeID, err)
			}
		}

		routePath := "/opt/warpgate/traefik/dynamic/" + appName + ".yml"
		if _, _, err := client.RunCommand("rm -f " + routePath); err != nil {
			d.log.Warnf("Failed to remove internal route on %s: %v", nodeID, err)
		}

		releaseLock(client, remoteDir, d.log)

		if _, _, err := client.RunCommand("rm -rf " + remoteDir); err != nil {
			d.log.Warnf("Failed to remove app directory on %s: %v", nodeID, err)
		}

		client.Close()

		version := state.CurrentVersion
		if version == "" {
			version = "none"
		}
		d.log.Infof("Removed %s from %s (was %s on %s slot)", appName, nodeID, version, state.ActiveSlot)
		removed = append(removed, nodeID)
	}

	if len(removed) == 0 {
		return fmt.Errorf("failed to remove %s from any node", appName)
	}

	d.log.Infof("Removed %s from %d node(s): %v", appName, len(removed), removed)
	return nil
}

// BreakLock forcibly removes a deploy lock for an app across its target nodes.
func (d *Deployer) BreakLock(appName string) error {
	app := d.Repo.GetApp(appName)
	var nodeIDs []string
	if app == nil {
		for _, node := range d.Repo.Cluster.Nodes {
			nodeIDs = append(nodeIDs, node.ID)
		}
	} else {
		nodeIDs = app.GetTargetNodes(d.Repo.Cluster.Nodes)
	}

	for _, nodeID := range nodeIDs {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s: %v", nodeID, err)
			continue
		}

		remoteDir := remoteAppsDir + "/" + appName
		info, err := breakLock(client, remoteDir, d.log)
		client.Close()

		if err != nil {
			d.log.Warnf("Failed to break lock on %s: %v", nodeID, err)
		} else if info != nil {
			d.log.Infof("Broke lock on %s (held by %s@%s since %s)", nodeID, info.User, info.Host, info.AcquiredAt.Format(time.RFC3339))
		} else {
			d.log.Infof("No lock found on %s", nodeID)
		}
	}

	return nil
}

// Status queries the deployment status of an app across its target nodes.
func (d *Deployer) Status(appName string) ([]NodeStatus, error) {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return nil, fmt.Errorf("app '%s' not found", appName)
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	var statuses []NodeStatus

	for _, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			statuses = append(statuses, NodeStatus{NodeID: nodeID, Error: err.Error()})
			continue
		}

		remoteDir := remoteAppsDir + "/" + appName
		state := d.readState(client, remoteDir)

		status := NodeStatus{
			NodeID:  nodeID,
			Version: state.CurrentVersion,
			Slot:    state.ActiveSlot,
		}

		composeFiles := "-f compose.yml -f docker-compose.override.yml"
		_, stdout, ok := findProjectPS(client, remoteDir, appName, state.ActiveSlot, composeFiles, "{{.Name}}\t{{.Status}}")
		if ok {
			status.State = "running"
			status.Containers = stdout
		} else {
			status.State = "not deployed"
		}

		if state.ShadowVersion != "" {
			status.ShadowVersion = state.ShadowVersion
			shadowProjectFlag := fmt.Sprintf("-p %s-%s", appName, shadowSlot)
			shadowComposeFiles := "-f compose.yml -f docker-compose.shadow-override.yml"
			shadowPsCmd := fmt.Sprintf("cd %s && docker compose %s %s ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
				remoteDir, shadowProjectFlag, shadowComposeFiles)
			shadowOut, _, shadowErr := client.RunCommand(shadowPsCmd)
			if shadowErr != nil || strings.TrimSpace(shadowOut) == "NOT_DEPLOYED" {
				status.ShadowState = "not deployed"
			} else {
				status.ShadowState = ParseContainerHealth(strings.TrimSpace(shadowOut))
			}
		}

		client.Close()
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// NodeStatus holds the deployment status for a single node.
type NodeStatus struct {
	// NodeID is the node identifier.
	NodeID string
	// State is the overall deployment state.
	State string
	// Version is the currently deployed version.
	Version string
	// Slot is the active blue/green slot. Recreate deployments leave this empty.
	Slot string
	// Containers is the docker compose ps output.
	Containers string
	// Error is set if the node could not be reached.
	Error string
	// ShadowVersion is the shadow deployment version, if any.
	ShadowVersion string
	// ShadowState is the shadow container state.
	ShadowState string
}

// ContainerStatus holds the status of a single container within a compose project.
type ContainerStatus struct {
	// Service is the compose service name.
	Service string
	// Name is the container name.
	Name string
	// State is the container health: "healthy", "running", "unhealthy".
	State string
}

// AppNodeStatus holds the deployment status of a single app on a single node.
type AppNodeStatus struct {
	// App is the application name.
	App string
	// NodeID is the node identifier.
	NodeID string
	// Version is the currently deployed version.
	Version string
	// Slot is the active blue/green slot. Recreate deployments leave this empty.
	Slot string
	// State is the deployment state: "healthy", "running", "unhealthy", "not deployed".
	State string
	// Services holds per-container status for release services.
	Services []ContainerStatus
	// Error is set if the status could not be determined.
	Error string
	// ShadowVersion is the shadow deployment version, if any.
	ShadowVersion string
	// ShadowState is the shadow container state.
	ShadowState string
}

// ClusterStatusResult holds the full cluster status across all nodes and apps.
type ClusterStatusResult struct {
	// NodeReachable maps node IDs to whether the node was reachable via SSH.
	NodeReachable map[string]bool
	// Apps holds the status of each app on each node.
	Apps []AppNodeStatus
}

// ClusterStatus queries the deployment status of all apps across all nodes.
func (d *Deployer) ClusterStatus() (*ClusterStatusResult, error) {
	result := &ClusterStatusResult{
		NodeReachable: make(map[string]bool),
	}

	for _, node := range d.Repo.Cluster.Nodes {
		client, err := d.connect(&node)
		if err != nil {
			result.NodeReachable[node.ID] = false
			for _, app := range d.Repo.GetAppsForNode(node.ID) {
				result.Apps = append(result.Apps, AppNodeStatus{
					App:    app.Name,
					NodeID: node.ID,
					Error:  err.Error(),
				})
			}
			continue
		}

		result.NodeReachable[node.ID] = true

		for _, app := range d.Repo.GetAppsForNode(node.ID) {
			remoteDir := remoteAppsDir + "/" + app.Name
			state := d.readState(client, remoteDir)

			status := AppNodeStatus{
				App:     app.Name,
				NodeID:  node.ID,
				Version: state.CurrentVersion,
				Slot:    state.ActiveSlot,
			}

			composeFiles := "-f compose.yml -f docker-compose.override.yml"
			_, stdout, ok := findProjectPS(client, remoteDir, app.Name, state.ActiveSlot, composeFiles, "{{.Service}}\t{{.Name}}\t{{.Status}}")
			if !ok {
				status.State = "not deployed"
				result.Apps = append(result.Apps, status)
				continue
			}

			status.State = ParseContainerHealth(stdout)
			status.Services = parseReleaseServiceStatuses(stdout, app.Release.Services)

			// Populate shadow status if a shadow is deployed.
			if state.ShadowVersion != "" {
				status.ShadowVersion = state.ShadowVersion

				shadowProjectFlag := fmt.Sprintf("-p %s-%s", app.Name, shadowSlot)
				shadowComposeFiles := "-f compose.yml -f docker-compose.shadow-override.yml"
				shadowPsCmd := fmt.Sprintf("cd %s && docker compose %s %s ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
					remoteDir, shadowProjectFlag, shadowComposeFiles)
				shadowOut, _, shadowErr := client.RunCommand(shadowPsCmd)
				if shadowErr != nil || strings.TrimSpace(shadowOut) == "NOT_DEPLOYED" {
					status.ShadowState = "not deployed"
				} else {
					status.ShadowState = ParseContainerHealth(strings.TrimSpace(shadowOut))
				}
			}

			result.Apps = append(result.Apps, status)
		}

		client.Close()
	}

	return result, nil
}

// ParseContainerHealth determines overall health from docker compose ps output.
// Each line has format: "container-name\tUp 2 minutes (healthy)"
// Returns "healthy" if all containers report healthy, "unhealthy" if any report unhealthy,
// or "running" if containers are up but have no health check.
func ParseContainerHealth(psOutput string) string {
	if psOutput == "" {
		return "not deployed"
	}

	hasHealthy := false
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "(unhealthy)") {
			return "unhealthy"
		}
		if strings.Contains(lower, "(healthy)") {
			hasHealthy = true
		}
	}

	if hasHealthy {
		return "healthy"
	}
	return "running"
}

// parseReleaseServiceStatuses extracts per-container status for release services
// from docker compose ps output. Each line has format: "service\tname\tstatus".
func parseReleaseServiceStatuses(psOutput string, services map[string]config.ReleaseServiceConfig) []ContainerStatus {
	if len(services) == 0 {
		return nil
	}

	var result []ContainerStatus
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		service, name, statusStr := parts[0], parts[1], parts[2]
		if _, ok := services[service]; !ok {
			continue
		}
		state := "running"
		lower := strings.ToLower(statusStr)
		if strings.Contains(lower, "(healthy)") {
			state = "healthy"
		} else if strings.Contains(lower, "(unhealthy)") {
			state = "unhealthy"
		}
		result = append(result, ContainerStatus{
			Service: service,
			Name:    name,
			State:   state,
		})
	}
	return result
}

func (d *Deployer) updateInternalProxy(app *config.AppConfig) error {
	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	for _, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil || node.PrivateIP == "" {
			continue
		}

		apps := d.Repo.GetAppsForNode(nodeID)
		entrypoints := compose.CollectInternalEntrypoints(apps)

		proxyCfg := &compose.InternalProxyConfig{
			PrivateIP:   node.PrivateIP,
			Entrypoints: entrypoints,
		}

		proxyYAML, err := compose.GenerateInternalProxyCompose(proxyCfg)
		if err != nil {
			d.log.Warnf("Failed to generate internal proxy for %s: %v", nodeID, err)
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s for internal proxy update: %v", nodeID, err)
			continue
		}

		remotePath := "/opt/warpgate/internal-proxy/compose.yml"
		if _, _, err := client.RunCommand("mkdir -p /opt/warpgate/internal-proxy"); err != nil {
			d.log.Warnf("Failed to create internal proxy directory on %s: %v", nodeID, err)
			client.Close()
			continue
		}

		if err := client.WriteFile(remotePath, proxyYAML); err != nil {
			d.log.Warnf("Failed to write internal proxy compose to %s: %v", nodeID, err)
			client.Close()
			continue
		}

		if _, _, err := client.RunCommand("cd /opt/warpgate/internal-proxy && docker compose -p warpgate-internal-proxy up -d"); err != nil {
			d.log.Warnf("Failed to start internal proxy on %s: %v", nodeID, err)
		}

		client.Close()
	}
	return nil
}

func (d *Deployer) updateReleaseServiceInternalRoutes(app *config.AppConfig, serviceName string, service config.ReleaseServiceConfig) error {
	routeYAML, err := GenerateReleaseServiceInternalRoute(app, serviceName, service, d.Repo.Cluster)
	if err != nil {
		return err
	}
	if routeYAML == "" {
		return nil
	}

	remotePath := "/opt/warpgate/traefik/dynamic/" + app.Name + "-" + serviceName + ".yml"
	d.log.Infof("Updating internal route for service %s.%s across all nodes...", app.Name, serviceName)

	for _, node := range d.Repo.Cluster.Nodes {
		client, err := d.connect(&node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s for service route update: %v", node.ID, err)
			continue
		}
		if err := client.WriteFile(remotePath, routeYAML); err != nil {
			d.log.Warnf("Failed to write service route to %s: %v", node.ID, err)
		}
		client.Close()
	}
	return nil
}

func (d *Deployer) uploadExtraFiles(uploader fileUploader, appDir, remoteDir string) error {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return fmt.Errorf("failed to read app directory: %w", err)
	}

	skip := map[string]bool{"app.yml": true, "compose.yml": true}
	for _, entry := range entries {
		if entry.IsDir() || skip[entry.Name()] {
			continue
		}
		localPath := filepath.Join(appDir, entry.Name())
		remotePath := remoteDir + "/" + entry.Name()
		d.log.Infof("Uploading %s", entry.Name())
		if err := uploader.UploadFile(localPath, remotePath); err != nil {
			return fmt.Errorf("failed to upload %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func (d *Deployer) connect(node *config.NodeConfig) (*ssh.Client, error) {
	user := d.User
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "root"
	}

	var client *ssh.Client
	if d.TailscaleSSH {
		client = ssh.NewTailscaleClient(node.Host, user)
	} else {
		var err error
		client, err = ssh.NewClient(node.Host, user, d.SSHKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSH client: %w", err)
		}
	}

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", node.Host, err)
	}

	return client, nil
}

func releaseNeedsEnvFile(manifest *release.Manifest, secretsServer string) bool {
	return len(releaseEnvFileMap(manifest, secretsServer)) > 0
}

func releaseEnvFileMap(manifest *release.Manifest, secretsServer string) map[string]bool {
	envFiles := make(map[string]bool)
	for serviceName, service := range manifest.Services {
		if len(service.Environment) > 0 {
			envFiles[serviceName] = true
		}
		if service.SecretsPrefix != "" && secretsServer != "" {
			envFiles[serviceName] = true
		}
	}
	return envFiles
}

func (d *Deployer) composeUpCommand(app *config.AppConfig, projectFlag, composeFiles string, hasEnvFile bool) string {
	base := "docker compose " + projectFlag + " " + composeFiles
	if hasEnvFile {
		base += " --env-file .env"
	}
	base += " up -d"
	return base
}

type commandRunner interface {
	RunCommand(cmd string) (string, string, error)
}

type deploymentRunner interface {
	commandRunner
	fileWriter
}

type stdinRunner interface {
	RunCommandStdin(cmd, stdinData string) (string, string, error)
}

type fileWriter interface {
	WriteFile(remotePath, content string) error
	WriteFileSecret(remotePath, content string) error
}

type fileUploader interface {
	UploadFile(localPath, remotePath string) error
}

type deploymentFS interface {
	fileWriter
	fileUploader
}

type healthWaitFunc func(commandRunner, string, string, string, string, logger) error

type deployPlan struct {
	activeSlot  string
	prevSlot    string
	projectFlag string
}

type deployResult struct {
	ActiveSlot string
}

type deploymentFileSnapshot struct {
	compose     string
	override    string
	hasCompose  bool
	hasOverride bool
}

func (s deploymentFileSnapshot) complete() bool {
	return s.hasCompose && s.hasOverride
}

type recreateRestorePlan struct {
	files    deploymentFileSnapshot
	manifest *release.Manifest
}

func (p recreateRestorePlan) canRestore() bool {
	return p.files.complete()
}

func makeDeployPlan(appName string, strategy config.DeployStrategy, state *DeployState) deployPlan {
	if state == nil {
		state = &DeployState{}
	}
	if strategy == config.StrategyRecreate {
		return deployPlan{
			projectFlag: deploymentProjectFlag(appName, strategy, ""),
		}
	}

	nextSlot := state.InactiveSlot()
	return deployPlan{
		activeSlot:  nextSlot,
		prevSlot:    state.ActiveSlot,
		projectFlag: deploymentProjectFlag(appName, strategy, nextSlot),
	}
}

func activeProjectFlag(appName string, strategy config.DeployStrategy, activeSlot string) (string, bool) {
	if strategy == config.StrategyRecreate {
		return deploymentProjectFlag(appName, strategy, ""), true
	}
	if activeSlot == "" {
		return "", false
	}
	return deploymentProjectFlag(appName, strategy, activeSlot), true
}

func statusProjectFlags(appName, activeSlot string) []string {
	var flags []string
	add := func(flag string) {
		for _, existing := range flags {
			if existing == flag {
				return
			}
		}
		flags = append(flags, flag)
	}

	if activeSlot != "" {
		add(deploymentProjectFlag(appName, config.StrategyBlueGreen, activeSlot))
	}
	add(deploymentProjectFlag(appName, config.StrategyRecreate, ""))
	add(deploymentProjectFlag(appName, config.StrategyBlueGreen, "blue"))
	add(deploymentProjectFlag(appName, config.StrategyBlueGreen, "green"))
	return flags
}

func deploymentProjectFlag(appName string, strategy config.DeployStrategy, slot string) string {
	projectName := appName
	if strategy != config.StrategyRecreate {
		projectName += "-" + slot
	}
	return "-p " + projectName
}

func allDeploymentProjectFlags(appName string) []string {
	return []string{
		deploymentProjectFlag(appName, config.StrategyRecreate, ""),
		deploymentProjectFlag(appName, config.StrategyBlueGreen, "blue"),
		deploymentProjectFlag(appName, config.StrategyBlueGreen, "green"),
	}
}

func queryProjectPS(runner commandRunner, remoteDir, projectFlag, composeFiles, format string) (string, bool) {
	psCmd := fmt.Sprintf("cd %s && docker compose %s %s ps --format %s 2>/dev/null || echo 'NOT_DEPLOYED'",
		remoteDir, projectFlag, composeFiles, shellQuote(format))
	stdout, _, err := runner.RunCommand(psCmd)
	if err != nil {
		return "", false
	}
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" || trimmed == "NOT_DEPLOYED" {
		return "", false
	}
	return trimmed, true
}

func findProjectPS(runner commandRunner, remoteDir, appName, activeSlot, composeFiles, format string) (string, string, bool) {
	for _, projectFlag := range statusProjectFlags(appName, activeSlot) {
		output, ok := queryProjectPS(runner, remoteDir, projectFlag, composeFiles, format)
		if ok {
			return projectFlag, output, true
		}
	}
	return "", "", false
}

func (d *Deployer) uploadDeploymentFiles(fs deploymentFS, remoteDir, appDir string, composeContent []byte, override string) error {
	if err := fs.WriteFile(remoteDir+"/compose.yml", string(composeContent)); err != nil {
		return fmt.Errorf("failed to upload compose.yml: %w", err)
	}
	if appDir != "" {
		if err := d.uploadExtraFiles(fs, appDir, remoteDir); err != nil {
			return err
		}
	}
	if err := fs.WriteFile(remoteDir+"/docker-compose.override.yml", override); err != nil {
		return fmt.Errorf("failed to write override: %w", err)
	}
	return nil
}

func (d *Deployer) prepareRecreateRestore(runner commandRunner, remoteDir string, app *config.AppConfig, state *DeployState) (recreateRestorePlan, error) {
	files, err := captureDeploymentFiles(runner, remoteDir)
	if err != nil {
		return recreateRestorePlan{}, fmt.Errorf("prepare recreate restore: %w", err)
	}

	hasRecordedDeployment := state != nil && (state.CurrentRelease != "" || state.CurrentVersion != "")
	if hasRecordedDeployment && !files.complete() {
		return recreateRestorePlan{}, fmt.Errorf("cannot prepare recreate restore for %s: previous compose files are missing", app.Name)
	}

	plan := recreateRestorePlan{
		files: files,
	}
	if state == nil || state.CurrentRelease == "" {
		return plan, nil
	}

	manifest, err := release.Load(d.Repo.AppReleasesDir(app.Name), state.CurrentRelease)
	if err != nil {
		return recreateRestorePlan{}, fmt.Errorf("cannot prepare recreate restore for %s release %s: %w", app.Name, state.CurrentRelease, err)
	}
	if manifest.App != "" && manifest.App != app.Name {
		return recreateRestorePlan{}, fmt.Errorf("cannot prepare recreate restore for %s: release %s belongs to app %s", app.Name, state.CurrentRelease, manifest.App)
	}
	plan.manifest = manifest
	return plan, nil
}

func captureDeploymentFiles(runner commandRunner, remoteDir string) (deploymentFileSnapshot, error) {
	composeContent, hasCompose, err := readRemoteTextFile(runner, remoteDir+"/compose.yml")
	if err != nil {
		return deploymentFileSnapshot{}, fmt.Errorf("read previous compose.yml: %w", err)
	}

	overrideContent, hasOverride, err := readRemoteTextFile(runner, remoteDir+"/docker-compose.override.yml")
	if err != nil {
		return deploymentFileSnapshot{}, fmt.Errorf("read previous docker-compose.override.yml: %w", err)
	}

	return deploymentFileSnapshot{
		compose:     composeContent,
		override:    overrideContent,
		hasCompose:  hasCompose,
		hasOverride: hasOverride,
	}, nil
}

func readRemoteTextFile(runner commandRunner, path string) (string, bool, error) {
	stdout, stderr, err := runner.RunCommand(readRemoteTextFileCommand(path))
	if err != nil {
		if stderr != "" {
			return "", false, fmt.Errorf("%w\n%s", err, stderr)
		}
		return "", false, err
	}

	present, content, ok := strings.Cut(stdout, "\n")
	if !ok {
		return "", false, fmt.Errorf("unexpected read response for %s", path)
	}
	switch present {
	case "1":
		return content, true, nil
	case "0":
		return "", false, nil
	default:
		return "", false, fmt.Errorf("unexpected read response for %s", path)
	}
}

func readRemoteTextFileCommand(path string) string {
	quotedPath := shellQuote(path)
	return fmt.Sprintf("if [ -f %s ]; then printf '1\\n'; cat %s; else printf '0\\n'; fi", quotedPath, quotedPath)
}

func (d *Deployer) writeReleaseEnvFile(writer fileWriter, remoteDir string, manifest *release.Manifest) (bool, error) {
	services := manifest.Services
	envFiles := releaseEnvFileMap(manifest, d.Repo.Cluster.Secrets.Server)
	if len(envFiles) == 0 {
		return false, nil
	}

	mergedProjectEnv := make(map[string]string)
	for _, serviceName := range sortedManifestServiceNames(services) {
		if !envFiles[serviceName] {
			continue
		}
		service := services[serviceName]
		envContent, mergedEnv, err := d.fetchServiceEnv(serviceName, service.Environment, service.SecretsPrefix)
		if err != nil {
			return false, err
		}
		for key, value := range mergedEnv {
			mergedProjectEnv[key] = value
		}
		envPath := remoteDir + "/" + serviceEnvFile(serviceName)
		if err := writer.WriteFileSecret(envPath, envContent); err != nil {
			return false, fmt.Errorf("failed to write %s: %w", serviceEnvFile(serviceName), err)
		}
	}

	if err := writer.WriteFileSecret(remoteDir+"/.env", secrets.FormatDotEnv(mergedProjectEnv)); err != nil {
		return false, fmt.Errorf("failed to write .env: %w", err)
	}
	return true, nil
}

func (d *Deployer) loginRegistry(runner stdinRunner) error {
	reg := d.Repo.Cluster.Registry
	if reg.Username == "" && d.Repo.Cluster.Secrets.Server != "" {
		fetched, err := d.fetchRegistryCredentials()
		if err != nil {
			d.log.Warnf("Failed to fetch registry credentials from SecretSauce: %v", err)
		} else if fetched != nil {
			reg = *fetched
		}
	}
	if reg.Username == "" {
		return nil
	}

	loginCmd := fmt.Sprintf("docker login %s -u %s --password-stdin",
		shellQuote(reg.Server),
		shellQuote(reg.Username))
	if _, _, err := runner.RunCommandStdin(loginCmd, reg.Password); err != nil {
		d.log.Warnf("Docker login failed: %v", err)
	}
	return nil
}

func (d *Deployer) deployWithStrategy(runner commandRunner, remoteDir string, app *config.AppConfig, state *DeployState, composeFiles string, hasEnvFile bool, waitForHealth healthWaitFunc) (deployResult, error) {
	plan := makeDeployPlan(app.Name, app.Strategy, state)
	if app.Strategy == config.StrategyRecreate {
		return d.runRecreateDeployment(runner, remoteDir, app, composeFiles, hasEnvFile, plan, waitForHealth)
	}
	return d.runBlueGreenDeployment(runner, remoteDir, app, composeFiles, hasEnvFile, plan, waitForHealth)
}

func (d *Deployer) recoverFailedRecreateDeployment(runner deploymentRunner, remoteDir string, app *config.AppConfig, restorePlan recreateRestorePlan, composeFiles string, hasEnvFile bool, deployErr error, waitForHealth healthWaitFunc) error {
	if deployErr == nil {
		return nil
	}
	if app.Strategy != config.StrategyRecreate || !restorePlan.canRestore() {
		return deployErr
	}

	if restoreErr := d.restorePreviousRecreateDeployment(runner, remoteDir, app, restorePlan, composeFiles, hasEnvFile, waitForHealth); restoreErr != nil {
		return fmt.Errorf("%w; restore previous deployment failed: %v", deployErr, restoreErr)
	}
	return fmt.Errorf("%w; previous deployment restored", deployErr)
}

func (d *Deployer) restorePreviousRecreateDeployment(runner deploymentRunner, remoteDir string, app *config.AppConfig, restorePlan recreateRestorePlan, composeFiles string, fallbackHasEnvFile bool, waitForHealth healthWaitFunc) error {
	d.log.Warnf("Restoring previous deployment for %s...", app.Name)
	if err := runner.WriteFile(remoteDir+"/compose.yml", restorePlan.files.compose); err != nil {
		return fmt.Errorf("restore compose.yml: %w", err)
	}
	if err := runner.WriteFile(remoteDir+"/docker-compose.override.yml", restorePlan.files.override); err != nil {
		return fmt.Errorf("restore docker-compose.override.yml: %w", err)
	}

	hasEnvFile := fallbackHasEnvFile
	if restorePlan.manifest != nil {
		var err error
		hasEnvFile, err = d.writeReleaseEnvFile(runner, remoteDir, restorePlan.manifest)
		if err != nil {
			return fmt.Errorf("write previous release env files: %w", err)
		}
	}

	projectFlag := deploymentProjectFlag(app.Name, config.StrategyRecreate, "")
	if err := d.startAndVerifyProject(runner, remoteDir, app, projectFlag, composeFiles, hasEnvFile, "previous "+app.Name, waitForHealth); err != nil {
		return fmt.Errorf("restart previous deployment: %w", err)
	}

	if restorePlan.manifest != nil && hasEnvFile {
		if err := removeReleaseEnvFiles(runner, remoteDir, restorePlan.manifest); err != nil {
			d.log.Warnf("Failed to remove restored env files: %v", err)
		}
	}
	d.log.Warnf("Restored previous deployment for %s", app.Name)
	return nil
}

func (d *Deployer) runRecreateDeployment(runner commandRunner, remoteDir string, app *config.AppConfig, composeFiles string, hasEnvFile bool, plan deployPlan, waitForHealth healthWaitFunc) (deployResult, error) {
	if err := d.pullProject(runner, remoteDir, plan.projectFlag, composeFiles); err != nil {
		return deployResult{}, err
	}

	d.log.Infof("Recreate strategy: stopping existing deployment before starting %s...", app.Name)
	for _, projectFlag := range allDeploymentProjectFlags(app.Name) {
		d.stopProject(runner, remoteDir, projectFlag, composeFiles, fmt.Sprintf("Failed to stop old deployment %s", projectFlag))
	}

	if err := d.startAndVerifyProject(runner, remoteDir, app, plan.projectFlag, composeFiles, hasEnvFile, app.Name, waitForHealth); err != nil {
		return deployResult{}, err
	}
	return deployResult{}, nil
}

func (d *Deployer) runBlueGreenDeployment(runner commandRunner, remoteDir string, app *config.AppConfig, composeFiles string, hasEnvFile bool, plan deployPlan, waitForHealth healthWaitFunc) (deployResult, error) {
	if err := d.pullProject(runner, remoteDir, plan.projectFlag, composeFiles); err != nil {
		return deployResult{}, err
	}

	if err := d.startAndVerifyProject(runner, remoteDir, app, plan.projectFlag, composeFiles, hasEnvFile, plan.activeSlot+" slot", waitForHealth); err != nil {
		return deployResult{}, err
	}

	if plan.prevSlot != "" {
		prevProjectFlag := deploymentProjectFlag(app.Name, app.Strategy, plan.prevSlot)
		d.log.Infof("Stopping %s slot...", plan.prevSlot)
		d.stopProject(runner, remoteDir, prevProjectFlag, composeFiles, "Failed to stop old slot")
	}

	return deployResult{ActiveSlot: plan.activeSlot}, nil
}

func (d *Deployer) pullProject(runner commandRunner, remoteDir, projectFlag, composeFiles string) error {
	pullCmd := fmt.Sprintf("cd %s && docker compose %s %s pull", remoteDir, projectFlag, composeFiles)
	d.log.Info("Pulling images...")
	if _, stderr, err := runner.RunCommand(pullCmd); err != nil {
		return fmt.Errorf("pull failed: %w\n%s", err, stderr)
	}
	return nil
}

func (d *Deployer) startAndVerifyProject(runner commandRunner, remoteDir string, app *config.AppConfig, projectFlag, composeFiles string, hasEnvFile bool, label string, waitForHealth healthWaitFunc) error {
	upCmd := d.composeUpCommand(app, projectFlag, composeFiles, hasEnvFile)
	fullCmd := fmt.Sprintf("cd %s && %s", remoteDir, upCmd)
	d.log.Infof("Starting %s...", label)
	if _, stderr, err := runner.RunCommand(fullCmd); err != nil {
		return fmt.Errorf("deploy failed: %w\n%s", err, stderr)
	}

	if err := waitForHealth(runner, remoteDir, app.Name, projectFlag, composeFiles, d.log); err != nil {
		d.log.Warnf("Health check failed, stopping %s...", label)
		d.stopProject(runner, remoteDir, projectFlag, composeFiles, "Failed to stop failed deployment")
		return fmt.Errorf("health check failed: %w", err)
	}
	return nil
}

func (d *Deployer) stopProject(runner commandRunner, remoteDir, projectFlag, composeFiles, warning string) {
	stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down", remoteDir, projectFlag, composeFiles)
	if _, _, err := runner.RunCommand(stopCmd); err != nil {
		d.log.Warnf("%s: %v", warning, err)
	}
}

func (d *Deployer) saveDeployState(client *ssh.Client, remoteDir string, app *config.AppConfig, manifest *release.Manifest, currentState *DeployState, activeSlot string) {
	newState := &DeployState{
		App:             app.Name,
		CurrentRelease:  manifest.ID,
		PreviousRelease: currentState.CurrentRelease,
		CurrentReleaseInputs: ReleaseInputs{
			ImageRef:   manifest.ImageRef,
			ComposeRev: manifest.ComposeRev,
			EnvHash:    manifest.EnvHash,
		},
		PreviousReleaseInputs: currentState.CurrentReleaseInputs,
		CurrentVersion:        manifest.ImageTag,
		PreviousVersion:       currentState.CurrentVersion,
		ActiveSlot:            activeSlot,
		DeployedAt:            time.Now(),
	}
	stateJSON, err := newState.Marshal()
	if err != nil {
		d.log.Warnf("Failed to marshal deploy state: %v", err)
		return
	}
	if err := client.WriteFile(remoteDir+"/state.json", stateJSON); err != nil {
		d.log.Warnf("Failed to save deploy state: %v", err)
	}
}

func (d *Deployer) fetchServiceEnv(serviceName string, environment map[string]string, secretsPrefix string) (string, map[string]string, error) {
	var secretEnv map[string]string
	if secretsPrefix != "" && d.Repo.Cluster.Secrets.Server != "" {
		client := secrets.NewClient(d.Repo.Cluster.Secrets.Server)
		d.log.Infof("Fetching secrets for service %s prefix %s...", serviceName, secretsPrefix)
		var err error
		secretEnv, err = client.FetchEnv(secretsPrefix)
		if err != nil {
			return "", nil, fmt.Errorf("failed to fetch secrets for service %s: %w", serviceName, err)
		}
		d.log.Infof("Fetched %d secret(s) for service %s", len(secretEnv), serviceName)
	}

	merged := mergeEnvironment(environment, secretEnv)
	return secrets.FormatDotEnv(merged), merged, nil
}

func sortedManifestServiceNames(services map[string]release.ServiceManifest) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedReleaseServiceConfigKeys(services map[string]config.ReleaseServiceConfig) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func serviceEnvFile(serviceName string) string {
	return ".env." + serviceName
}

func releaseEnvPaths(remoteDir string, manifest *release.Manifest) []string {
	services := manifest.Services
	paths := []string{remoteDir + "/.env"}
	for _, serviceName := range sortedManifestServiceNames(services) {
		paths = append(paths, remoteDir+"/"+serviceEnvFile(serviceName))
	}
	return paths
}

func removeReleaseEnvFiles(runner commandRunner, remoteDir string, manifest *release.Manifest) error {
	if _, _, err := runner.RunCommand("rm -f " + strings.Join(releaseEnvPaths(remoteDir, manifest), " ")); err != nil {
		return fmt.Errorf("failed to remove .env file: %w", err)
	}
	return nil
}

// mergeEnvironment combines app-level environment variables with secrets.
// Secrets take precedence over environment variables on key collision.
func mergeEnvironment(env, secrets map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range env {
		merged[k] = v
	}
	for k, v := range secrets {
		merged[k] = v
	}
	return merged
}

// fetchRegistryCredentials retrieves Docker registry credentials from SecretSauce.
func (d *Deployer) fetchRegistryCredentials() (*config.RegistryConfig, error) {
	client := secrets.NewClient(d.Repo.Cluster.Secrets.Server)
	d.log.Info("Fetching registry credentials from SecretSauce...")
	env, err := client.FetchEnv(secrets.RegistryPrefix)
	if err != nil {
		return nil, err
	}
	if len(env) == 0 {
		return nil, nil
	}
	reg := &config.RegistryConfig{
		Server:   env["SERVER"],
		Username: env["USERNAME"],
		Password: env["PASSWORD"],
	}
	d.log.Info("Using registry credentials from SecretSauce")
	return reg, nil
}

func (d *Deployer) readState(client *ssh.Client, remoteDir string) *DeployState {
	stdout, _, err := client.RunCommand(fmt.Sprintf("cat %s/state.json 2>/dev/null || echo '{}'", remoteDir))
	if err != nil {
		return &DeployState{}
	}
	state, err := UnmarshalState(strings.TrimSpace(stdout))
	if err != nil {
		return &DeployState{}
	}
	return state
}

// shellQuote wraps a value in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
