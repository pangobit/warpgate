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
	"github.com/pangobit/warpgate/pkg/secrets"
	"github.com/pangobit/warpgate/pkg/ssh"
	"github.com/sirupsen/logrus"
)

func sortedKeys(m map[string]config.SidecarConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

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
		app.Version = version
	}

	appDir := d.Repo.AppDir(appName)
	composePath := d.Repo.AppComposePath(appName)

	var composeContent []byte
	var err error
	if app.Source != nil {
		d.log.Infof("Fetching compose from %s@%s...", app.Source.Repo, app.Source.Ref)
		composeContent, err = FetchComposeFromSource(app.Source, d.GitHubToken)
		if err != nil {
			return fmt.Errorf("failed to fetch remote compose: %w", err)
		}
		appDir = ""
	} else {
		if _, err = os.Stat(composePath); os.IsNotExist(err) {
			return fmt.Errorf("compose.yml not found for app '%s' at %s", appName, composePath)
		}
		composeContent, err = os.ReadFile(composePath)
		if err != nil {
			return fmt.Errorf("failed to read compose.yml: %w", err)
		}
	}

	override, err := compose.GenerateOverride(app, &d.Repo.Cluster.Networking, d.Repo.InternalHosts(), string(composeContent))
	if err != nil {
		return fmt.Errorf("failed to generate override: %w", err)
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	d.log.Infof("Deploying %s:%s to nodes: %v", app.Image, app.Version, targetNodes)

	if d.DryRun {
		if app.Strategy == config.StrategyRecreate {
			d.log.Info("DRY RUN — recreate deploy (brief downtime)")
		} else {
			d.log.Info("DRY RUN — zero-downtime deploy via blue/green swap")
		}
		fmt.Println()
		fmt.Println("Override that will be applied:")
		fmt.Println(override)
		fmt.Printf("Targets: %v\n", targetNodes)
		if app.Source != nil {
			fmt.Printf("Compose source: %s@%s (path: %s)\n", app.Source.Repo, app.Source.Ref, app.Source.ComposePath)
		}
		needsSecrets := app.SecretsPrefix != "" && d.Repo.Cluster.Secrets.Server != ""
		if needsSecrets {
			fmt.Printf("Secrets: fetch from %s (prefix: %s) → .env file\n", d.Repo.Cluster.Secrets.Server, app.SecretsPrefix)
		}
		if len(app.Environment) > 0 {
			fmt.Printf("Environment: %d variable(s) from app.yml → .env file\n", len(app.Environment))
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

		if err := d.deployToNode(app, node, appDir, composeContent, override); err != nil {
			d.log.Warnf("Deploy to node %s failed: %v", nodeID, err)
			if len(deployed) > 0 {
				d.log.Warnf("Nodes already updated: %v", deployed)
			}
			return fmt.Errorf("rolling deploy stopped at node %s (%d/%d): %w", nodeID, i+1, len(targetNodes), err)
		}

		deployed = append(deployed, nodeID)
	}

	if app.EffectiveExpose().Internal != nil {
		if err := d.updateInternalRoutes(app); err != nil {
			d.log.Warnf("Failed to update internal routes: %v", err)
		}
	}

	for _, sidecarName := range sortedKeys(app.Sidecars) {
		sidecar := app.Sidecars[sidecarName]
		if sidecar.EffectiveExpose().Internal != nil {
			if err := d.updateSidecarInternalRoutes(app, sidecarName, sidecar); err != nil {
				d.log.Warnf("Failed to update internal routes for sidecar %s: %v", sidecarName, err)
			}
		}
	}

	if err := d.updateInternalProxy(app); err != nil {
		d.log.Warnf("Failed to update internal proxy: %v", err)
	}

	d.log.Infof("Deploy complete: %s:%s across %d node(s)", app.Name, app.Version, len(deployed))
	return nil
}

func (d *Deployer) deployToNode(app *config.AppConfig, node *config.NodeConfig, appDir string, composeContent []byte, override string) error {
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
	nextSlot := currentState.InactiveSlot()
	prevSlot := currentState.ActiveSlot

	if err := client.WriteFile(remoteDir+"/compose.yml", string(composeContent)); err != nil {
		return fmt.Errorf("failed to upload compose.yml: %w", err)
	}

	if appDir != "" {
		if err := d.uploadExtraFiles(client, appDir, remoteDir); err != nil {
			return err
		}
	}

	if err := client.WriteFile(remoteDir+"/docker-compose.override.yml", override); err != nil {
		return fmt.Errorf("failed to write override: %w", err)
	}

	hasEnvFile := false
	if needsEnvFile(app, d.Repo.Cluster.Secrets.Server) {
		envContent, fetchErr := d.fetchSecrets(app)
		if fetchErr != nil {
			return fmt.Errorf("failed to fetch secrets: %w", fetchErr)
		}
		envPath := remoteDir + "/.env"
		if err := client.WriteFileSecret(envPath, envContent); err != nil {
			return fmt.Errorf("failed to write .env: %w", err)
		}
		hasEnvFile = true
	}

	reg := d.Repo.Cluster.Registry
	if reg.Username == "" && d.Repo.Cluster.Secrets.Server != "" {
		fetched, fetchErr := d.fetchRegistryCredentials()
		if fetchErr != nil {
			d.log.Warnf("Failed to fetch registry credentials from SecretSauce: %v", fetchErr)
		} else if fetched != nil {
			reg = *fetched
		}
	}
	if reg.Username != "" {
		loginCmd := fmt.Sprintf("docker login %s -u %s --password-stdin",
			shellQuote(reg.Server),
			shellQuote(reg.Username))
		if _, _, err := client.RunCommandStdin(loginCmd, reg.Password); err != nil {
			d.log.Warnf("Docker login failed: %v", err)
		}
	}

	composeFiles := "-f compose.yml -f docker-compose.override.yml"
	projectFlag := fmt.Sprintf("-p %s-%s", app.Name, nextSlot)

	pullCmd := fmt.Sprintf("cd %s && docker compose %s %s pull", remoteDir, projectFlag, composeFiles)
	d.log.Info("Pulling images...")
	if _, stderr, err := client.RunCommand(pullCmd); err != nil {
		return fmt.Errorf("pull failed: %w\n%s", err, stderr)
	}

	if app.Strategy == config.StrategyRecreate && prevSlot != "" {
		prevProjectFlag := fmt.Sprintf("-p %s-%s", app.Name, prevSlot)
		d.log.Infof("Recreate strategy: stopping %s slot before starting %s...", prevSlot, nextSlot)
		stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down", remoteDir, prevProjectFlag, composeFiles)
		if _, _, err := client.RunCommand(stopCmd); err != nil {
			d.log.Warnf("Failed to stop old slot: %v", err)
		}
		prevSlot = "" // already stopped, skip the post-healthcheck stop
	}

	upCmd := d.composeUpCommand(app, projectFlag, composeFiles, hasEnvFile)
	fullCmd := fmt.Sprintf("cd %s && %s", remoteDir, upCmd)
	d.log.Infof("Starting %s slot...", nextSlot)
	if _, stderr, err := client.RunCommand(fullCmd); err != nil {
		return fmt.Errorf("deploy failed: %w\n%s", err, stderr)
	}

	if err := WaitForHealthy(client, remoteDir, app.Name, projectFlag, composeFiles, d.log); err != nil {
		d.log.Warnf("Health check failed, stopping %s slot...", nextSlot)
		stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down", remoteDir, projectFlag, composeFiles)
		if _, _, stopErr := client.RunCommand(stopCmd); stopErr != nil {
			d.log.Warnf("Failed to stop failed slot: %v", stopErr)
		}
		return fmt.Errorf("health check failed: %w", err)
	}

	if hasEnvFile {
		if _, _, err := client.RunCommand("rm -f " + remoteDir + "/.env"); err != nil {
			return fmt.Errorf("failed to remove .env file: %w", err)
		}
	}

	if prevSlot != "" {
		prevProjectFlag := fmt.Sprintf("-p %s-%s", app.Name, prevSlot)
		d.log.Infof("Stopping %s slot...", prevSlot)
		stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down", remoteDir, prevProjectFlag, composeFiles)
		if _, _, err := client.RunCommand(stopCmd); err != nil {
			d.log.Warnf("Failed to stop old slot: %v", err)
		}
	}

	newState := &DeployState{
		App:             app.Name,
		CurrentVersion:  app.Version,
		PreviousVersion: currentState.CurrentVersion,
		ActiveSlot:      nextSlot,
		DeployedAt:      time.Now(),
	}
	stateJSON, err := newState.Marshal()
	if err != nil {
		d.log.Warnf("Failed to marshal deploy state: %v", err)
	} else if err := client.WriteFile(remoteDir+"/state.json", stateJSON); err != nil {
		d.log.Warnf("Failed to save deploy state: %v", err)
	}

	d.log.Infof("Node %s: %s slot active, %s:%s deployed", node.ID, nextSlot, app.Name, app.Version)
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

	if state.PreviousVersion == "" {
		return fmt.Errorf("no previous version found for %s", appName)
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

		for _, slot := range []string{"blue", "green"} {
			projectFlag := fmt.Sprintf("-p %s-%s", appName, slot)
			stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down 2>/dev/null || true", remoteDir, projectFlag, composeFiles)
			if _, _, err := client.RunCommand(stopCmd); err != nil {
				d.log.Warnf("Failed to stop %s slot on %s: %v", slot, nodeID, err)
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

		if state.ActiveSlot != "" {
			projectFlag := fmt.Sprintf("-p %s-%s", appName, state.ActiveSlot)
			composeFiles := "-f compose.yml -f docker-compose.override.yml"
			psCmd := fmt.Sprintf("cd %s && docker compose %s %s ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
				remoteDir, projectFlag, composeFiles)
			stdout, _, err := client.RunCommand(psCmd)
			if err != nil || strings.TrimSpace(stdout) == "NOT_DEPLOYED" {
				status.State = "not deployed"
			} else {
				status.State = "running"
				status.Containers = strings.TrimSpace(stdout)
			}
		} else {
			status.State = "not deployed"
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
	// Slot is the active blue/green slot.
	Slot string
	// Containers is the docker compose ps output.
	Containers string
	// Error is set if the node could not be reached.
	Error string
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
	// Slot is the active blue/green slot.
	Slot string
	// State is the deployment state: "healthy", "running", "unhealthy", "not deployed".
	State string
	// Sidecars holds per-container status for sidecar services.
	Sidecars []ContainerStatus
	// Error is set if the status could not be determined.
	Error string
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

			if state.ActiveSlot == "" {
				status.State = "not deployed"
				result.Apps = append(result.Apps, status)
				continue
			}

			projectFlag := fmt.Sprintf("-p %s-%s", app.Name, state.ActiveSlot)
			composeFiles := "-f compose.yml -f docker-compose.override.yml"
			psCmd := fmt.Sprintf("cd %s && docker compose %s %s ps --format '{{.Service}}\t{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
				remoteDir, projectFlag, composeFiles)
			stdout, _, err := client.RunCommand(psCmd)
			if err != nil || strings.TrimSpace(stdout) == "NOT_DEPLOYED" {
				status.State = "not deployed"
			} else {
				trimmed := strings.TrimSpace(stdout)
				status.State = ParseContainerHealth(trimmed)
				status.Sidecars = parseSidecarStatuses(trimmed, app.Sidecars)
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

// parseSidecarStatuses extracts per-container status for sidecar services
// from docker compose ps output. Each line has format: "service\tname\tstatus".
func parseSidecarStatuses(psOutput string, sidecars map[string]config.SidecarConfig) []ContainerStatus {
	if len(sidecars) == 0 {
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
		if _, ok := sidecars[service]; !ok {
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

func (d *Deployer) updateSidecarInternalRoutes(app *config.AppConfig, sidecarName string, sidecar config.SidecarConfig) error {
	routeYAML, err := GenerateSidecarInternalRoute(app, sidecarName, sidecar, d.Repo.Cluster)
	if err != nil {
		return err
	}
	if routeYAML == "" {
		return nil
	}

	remotePath := "/opt/warpgate/traefik/dynamic/" + app.Name + "-" + sidecarName + ".yml"
	d.log.Infof("Updating internal route for sidecar %s.%s across all nodes...", app.Name, sidecarName)

	for _, node := range d.Repo.Cluster.Nodes {
		client, err := d.connect(&node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s for sidecar route update: %v", node.ID, err)
			continue
		}
		if err := client.WriteFile(remotePath, routeYAML); err != nil {
			d.log.Warnf("Failed to write sidecar route to %s: %v", node.ID, err)
		}
		client.Close()
	}
	return nil
}

func (d *Deployer) updateInternalRoutes(app *config.AppConfig) error {
	routeYAML, err := GenerateInternalRoute(app, d.Repo.Cluster)
	if err != nil {
		return err
	}
	if routeYAML == "" {
		return nil
	}

	remotePath := "/opt/warpgate/traefik/dynamic/" + app.Name + ".yml"
	d.log.Infof("Updating internal route for %s across all nodes...", app.EffectiveExpose().Internal.Hostname)

	for _, node := range d.Repo.Cluster.Nodes {
		client, err := d.connect(&node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s for internal route update: %v", node.ID, err)
			continue
		}

		if err := client.WriteFile(remotePath, routeYAML); err != nil {
			d.log.Warnf("Failed to write internal route to %s: %v", node.ID, err)
		}
		client.Close()
	}

	return nil
}

func (d *Deployer) uploadExtraFiles(client *ssh.Client, appDir, remoteDir string) error {
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
		if err := client.UploadFile(localPath, remotePath); err != nil {
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

// needsEnvFile reports whether the app requires a .env file on the remote node.
// This is true when the app has environment variables, fetchable secrets, or both.
func needsEnvFile(app *config.AppConfig, secretsServer string) bool {
	if len(app.Environment) > 0 {
		return true
	}
	return app.SecretsPrefix != "" && secretsServer != ""
}

func (d *Deployer) composeUpCommand(app *config.AppConfig, projectFlag, composeFiles string, hasEnvFile bool) string {
	base := "docker compose " + projectFlag + " " + composeFiles
	if hasEnvFile {
		base += " --env-file .env"
	}
	base += " up -d"
	return base
}

func (d *Deployer) fetchSecrets(app *config.AppConfig) (string, error) {
	var secretEnv map[string]string
	if app.SecretsPrefix != "" && d.Repo.Cluster.Secrets.Server != "" {
		client := secrets.NewClient(d.Repo.Cluster.Secrets.Server)
		d.log.Infof("Fetching secrets for prefix %s...", app.SecretsPrefix)
		var err error
		secretEnv, err = client.FetchEnv(app.SecretsPrefix)
		if err != nil {
			return "", err
		}
		d.log.Infof("Fetched %d secret(s)", len(secretEnv))
	}

	merged := mergeEnvironment(app.Environment, secretEnv)
	return secrets.FormatDotEnv(merged), nil
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
