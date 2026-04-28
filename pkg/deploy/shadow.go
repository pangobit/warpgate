package deploy

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/compose"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/release"
)

const shadowSlot = "shadow"

// ShadowDeploy deploys a shadow version of an app alongside the live deployment.
// The shadow runs on the same node(s) but is not wired to the public proxy —
// it is only reachable over the internal (Tailscale) network.
func (d *Deployer) ShadowDeploy(appName, version string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	if version == "" {
		return fmt.Errorf("shadow deploy requires a version")
	}

	if err := d.validateShadowPreconditions(app); err != nil {
		return err
	}

	appDir := d.Repo.AppDir(app.Name)
	composePath := d.Repo.AppComposePath(app.Name)

	var composeContent []byte
	var err error
	if app.Source != nil {
		d.log.Infof("Fetching compose from %s@%s...", app.Source.Repo, version)
		composeContent, err = FetchComposeFromSource(app.Source, version, d.GitHubToken)
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

	internalHosts := d.Repo.InternalHosts()
	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	manifest := release.Build(app, composeContent, time.Now())

	d.log.Infof("Deploying shadow %s:%s to nodes: %v", appName, version, targetNodes)

	for i, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			d.log.Warnf("Node '%s' not found in config, skipping", nodeID)
			continue
		}

		d.log.Infof("[%d/%d] Deploying shadow to node %s...", i+1, len(targetNodes), nodeID)

		envFiles := releaseEnvFileMap(manifest, d.Repo.Cluster.Secrets.Server)
		override, err := compose.GenerateShadowOverrideWithEnvFiles(app, version, envFiles, internalHosts, node.PrivateIP, string(composeContent))
		if err != nil {
			return fmt.Errorf("failed to generate shadow override for node %s: %w", nodeID, err)
		}

		if err := d.deployShadowToNode(app, manifest, node, appDir, composeContent, override, version); err != nil {
			return fmt.Errorf("shadow deploy to node %s failed: %w", nodeID, err)
		}
	}

	for _, serviceName := range sortedReleaseServiceConfigKeys(app.Release.Services) {
		service := app.Release.Services[serviceName]
		if service.EffectiveExpose().Internal != nil {
			if err := d.updateReleaseServiceShadowInternalRoutes(app, serviceName, service); err != nil {
				d.log.Warnf("Failed to update shadow internal routes for service %s: %v", serviceName, err)
			}
		}
	}

	d.log.Infof("Shadow deploy complete: %s:%s", appName, version)
	return nil
}

// validateShadowPreconditions checks that an app has a live deployment and no existing shadow.
func (d *Deployer) validateShadowPreconditions(app *config.AppConfig) error {
	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	if len(targetNodes) == 0 {
		return fmt.Errorf("no target nodes for %s", app.Name)
	}

	node := d.Repo.Cluster.GetNode(targetNodes[0])
	if node == nil {
		return fmt.Errorf("node %s not found", targetNodes[0])
	}

	client, err := d.connect(node)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", node.ID, err)
	}
	defer client.Close()

	remoteDir := remoteAppsDir + "/" + app.Name
	state := d.readState(client, remoteDir)

	if state.CurrentVersion == "" {
		return fmt.Errorf("app '%s' has no live deployment (deploy it first)", app.Name)
	}
	if state.ShadowVersion != "" {
		return fmt.Errorf("app '%s' already has a shadow deployment at %s (remove it first)", app.Name, state.ShadowVersion)
	}

	return nil
}

// deployShadowToNode deploys shadow containers to a single node.
func (d *Deployer) deployShadowToNode(app *config.AppConfig, manifest *release.Manifest, node *config.NodeConfig, appDir string, composeContent []byte, override string, version string) error {
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

	if err := client.WriteFile(remoteDir+"/compose.yml", string(composeContent)); err != nil {
		return fmt.Errorf("failed to upload compose.yml: %w", err)
	}

	if appDir != "" {
		if err := d.uploadExtraFiles(client, appDir, remoteDir); err != nil {
			return err
		}
	}

	shadowOverridePath := remoteDir + "/docker-compose.shadow-override.yml"
	if err := client.WriteFile(shadowOverridePath, override); err != nil {
		return fmt.Errorf("failed to write shadow override: %w", err)
	}

	hasEnvFile, err := d.writeReleaseEnvFile(client, remoteDir, manifest)
	if err != nil {
		return err
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

	composeFiles := "-f compose.yml -f docker-compose.shadow-override.yml"
	projectFlag := fmt.Sprintf("-p %s-%s", app.Name, shadowSlot)

	pullCmd := fmt.Sprintf("cd %s && docker compose %s %s pull", remoteDir, projectFlag, composeFiles)
	d.log.Info("Pulling shadow images...")
	if _, stderr, err := client.RunCommand(pullCmd); err != nil {
		return fmt.Errorf("shadow pull failed: %w\n%s", err, stderr)
	}

	upCmd := d.composeUpCommand(app, projectFlag, composeFiles, hasEnvFile)
	fullCmd := fmt.Sprintf("cd %s && %s", remoteDir, upCmd)
	d.log.Info("Starting shadow containers...")
	if _, stderr, err := client.RunCommand(fullCmd); err != nil {
		return fmt.Errorf("shadow deploy failed: %w\n%s", err, stderr)
	}

	if err := WaitForHealthy(client, remoteDir, app.Name, projectFlag, composeFiles, d.log); err != nil {
		d.log.Warn("Shadow health check failed, stopping shadow containers...")
		stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down", remoteDir, projectFlag, composeFiles)
		if _, _, stopErr := client.RunCommand(stopCmd); stopErr != nil {
			d.log.Warnf("Failed to stop failed shadow: %v", stopErr)
		}
		return fmt.Errorf("shadow health check failed: %w", err)
	}

	if hasEnvFile {
		if _, _, err := client.RunCommand("rm -f " + strings.Join(releaseEnvPaths(remoteDir, manifest), " ")); err != nil {
			return fmt.Errorf("failed to remove .env file: %w", err)
		}
	}

	currentState := d.readState(client, remoteDir)
	now := time.Now()
	currentState.ShadowVersion = version
	currentState.ShadowDeployedAt = &now

	stateJSON, err := currentState.Marshal()
	if err != nil {
		d.log.Warnf("Failed to marshal deploy state: %v", err)
	} else if err := client.WriteFile(remoteDir+"/state.json", stateJSON); err != nil {
		d.log.Warnf("Failed to save deploy state: %v", err)
	}

	d.log.Infof("Node %s: shadow %s:%s deployed", node.ID, app.Name, version)
	return nil
}

// ShadowPromote promotes a shadow deployment to live via a standard blue-green deploy,
// then tears down the shadow.
func (d *Deployer) ShadowPromote(appName string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	shadowVersion, err := d.getShadowVersion(app)
	if err != nil {
		return err
	}

	d.log.Infof("Promoting shadow %s:%s to live...", appName, shadowVersion)
	if err := d.Deploy(appName, shadowVersion); err != nil {
		return fmt.Errorf("promotion deploy failed: %w", err)
	}

	d.log.Info("Tearing down shadow...")
	d.cleanupShadow(appName)

	d.log.Infof("Shadow promote complete: %s is now live at %s", appName, shadowVersion)
	return nil
}

// getShadowVersion reads the shadow version from state.json.
func (d *Deployer) getShadowVersion(app *config.AppConfig) (string, error) {
	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	if len(targetNodes) == 0 {
		return "", fmt.Errorf("no target nodes for %s", app.Name)
	}

	node := d.Repo.Cluster.GetNode(targetNodes[0])
	if node == nil {
		return "", fmt.Errorf("node %s not found", targetNodes[0])
	}

	client, err := d.connect(node)
	if err != nil {
		return "", err
	}
	defer client.Close()

	remoteDir := remoteAppsDir + "/" + app.Name
	state := d.readState(client, remoteDir)
	if state.ShadowVersion == "" {
		return "", fmt.Errorf("app '%s' has no shadow deployment", app.Name)
	}

	return state.ShadowVersion, nil
}

// ShadowRemove tears down the shadow deployment for an app.
func (d *Deployer) ShadowRemove(appName string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	d.log.Infof("Removing shadow for %s...", appName)
	d.cleanupShadow(appName)
	d.log.Infof("Shadow removal complete for %s", appName)
	return nil
}

// cleanupShadow removes the shadow deployment for a single app on all target nodes.
func (d *Deployer) cleanupShadow(appName string) {
	app := d.Repo.GetApp(appName)
	if app == nil {
		d.log.Warnf("App '%s' not found, skipping shadow cleanup", appName)
		return
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)

	for _, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s for shadow cleanup: %v", nodeID, err)
			continue
		}

		remoteDir := remoteAppsDir + "/" + appName
		composeFiles := "-f compose.yml -f docker-compose.shadow-override.yml"
		projectFlag := fmt.Sprintf("-p %s-%s", appName, shadowSlot)

		stopCmd := fmt.Sprintf("cd %s && docker compose %s %s down 2>/dev/null || true", remoteDir, projectFlag, composeFiles)
		if _, _, err := client.RunCommand(stopCmd); err != nil {
			d.log.Warnf("Failed to stop shadow on %s: %v", nodeID, err)
		}

		if _, _, err := client.RunCommand("rm -f " + remoteDir + "/docker-compose.shadow-override.yml"); err != nil {
			d.log.Warnf("Failed to remove shadow override on %s: %v", nodeID, err)
		}

		state := d.readState(client, remoteDir)
		state.ShadowVersion = ""
		state.ShadowDeployedAt = nil
		stateJSON, err := state.Marshal()
		if err == nil {
			if writeErr := client.WriteFile(remoteDir+"/state.json", stateJSON); writeErr != nil {
				d.log.Warnf("Failed to update state on %s: %v", nodeID, writeErr)
			}
		}

		client.Close()
	}

	routePath := "/opt/warpgate/traefik/dynamic/" + appName + "-shadow.yml"
	for _, node := range d.Repo.Cluster.Nodes {
		client, err := d.connect(&node)
		if err != nil {
			continue
		}
		if _, _, err := client.RunCommand("rm -f " + routePath); err != nil {
			d.log.Warnf("Failed to remove shadow route on %s: %v", node.ID, err)
		}
		client.Close()
	}
}

// updateReleaseServiceShadowInternalRoutes writes the shadow Traefik dynamic config to all nodes.
func (d *Deployer) updateReleaseServiceShadowInternalRoutes(app *config.AppConfig, serviceName string, service config.ReleaseServiceConfig) error {
	routeYAML, err := GenerateReleaseServiceShadowInternalRoute(app, serviceName, service, d.Repo.Cluster)
	if err != nil {
		return err
	}
	if routeYAML == "" {
		return nil
	}

	remotePath := "/opt/warpgate/traefik/dynamic/" + app.Name + "-" + serviceName + "-shadow.yml"
	d.log.Infof("Updating shadow internal route for service %s.%s across all nodes...", app.Name, serviceName)

	for _, node := range d.Repo.Cluster.Nodes {
		client, err := d.connect(&node)
		if err != nil {
			d.log.Warnf("Failed to connect to %s for shadow route update: %v", node.ID, err)
			continue
		}
		if err := client.WriteFile(remotePath, routeYAML); err != nil {
			d.log.Warnf("Failed to write shadow route to %s: %v", node.ID, err)
		}
		client.Close()
	}

	return nil
}

// ShadowStatus queries the shadow deployment status of an app across its target nodes.
func (d *Deployer) ShadowStatus(appName string) ([]ShadowNodeStatus, error) {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return nil, fmt.Errorf("app '%s' not found", appName)
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	var statuses []ShadowNodeStatus

	for _, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			statuses = append(statuses, ShadowNodeStatus{NodeID: nodeID, Error: err.Error()})
			continue
		}

		remoteDir := remoteAppsDir + "/" + appName
		state := d.readState(client, remoteDir)

		status := ShadowNodeStatus{
			NodeID:     nodeID,
			Version:    state.ShadowVersion,
			DeployedAt: state.ShadowDeployedAt,
		}

		if state.ShadowVersion != "" {
			projectFlag := fmt.Sprintf("-p %s-%s", appName, shadowSlot)
			composeFiles := "-f compose.yml -f docker-compose.shadow-override.yml"
			psCmd := fmt.Sprintf("cd %s && docker compose %s %s ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'",
				remoteDir, projectFlag, composeFiles)
			stdout, _, err := client.RunCommand(psCmd)
			if err != nil || strings.TrimSpace(stdout) == "NOT_DEPLOYED" {
				status.State = "not deployed"
			} else {
				status.State = ParseContainerHealth(strings.TrimSpace(stdout))
				status.Containers = strings.TrimSpace(stdout)
			}
		} else {
			status.State = "no shadow"
		}

		client.Close()
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// ShadowNodeStatus holds the shadow deployment status for a single node.
type ShadowNodeStatus struct {
	// NodeID is the node identifier.
	NodeID string
	// State is the shadow deployment state.
	State string
	// Version is the shadow version.
	Version string
	// DeployedAt is the timestamp of the shadow deployment.
	DeployedAt *time.Time
	// Containers is the docker compose ps output.
	Containers string
	// Error is set if the node could not be reached.
	Error string
}
