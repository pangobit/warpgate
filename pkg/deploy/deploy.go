// Package deploy handles application deployment to remote nodes.
package deploy

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pangobit/warpgate/pkg/compose"
	"github.com/pangobit/warpgate/pkg/config"
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

// Deploy deploys an app to its target nodes.
func (d *Deployer) Deploy(appName, version string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	if version != "" {
		app.Version = version
	}

	composePath := d.Repo.AppComposePath(appName)
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return fmt.Errorf("compose.yml not found for app '%s' at %s", appName, composePath)
	}

	override, err := compose.GenerateOverride(app, &d.Repo.Cluster.Networking)
	if err != nil {
		return fmt.Errorf("failed to generate override: %w", err)
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)
	d.log.Infof("Deploying %s:%s to nodes: %v", app.Image, app.Version, targetNodes)

	if d.DryRun {
		d.log.Info("DRY RUN — actions that would be taken:")
		fmt.Printf("Upload: %s -> %s/%s/compose.yml\n", composePath, remoteAppsDir, appName)
		fmt.Printf("Write override: %s/%s/docker-compose.override.yml\n", remoteAppsDir, appName)
		fmt.Println("--- override content ---")
		fmt.Println(override)
		fmt.Println("---")
		cmd := d.composeUpCommand(app)
		fmt.Printf("Run: %s\n", cmd)
		return nil
	}

	for _, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			d.log.Warnf("Node '%s' not found in config, skipping", nodeID)
			continue
		}

		if err := d.deployToNode(app, node, composePath, override); err != nil {
			return fmt.Errorf("deploy to node %s failed: %w", nodeID, err)
		}
	}

	d.log.Infof("Deploy complete: %s:%s", app.Name, app.Version)
	return nil
}

func (d *Deployer) deployToNode(app *config.AppConfig, node *config.NodeConfig, composePath, override string) error {
	d.log.Infof("Deploying to node %s (%s)...", node.ID, node.Host)

	client, err := d.connect(node)
	if err != nil {
		return err
	}
	defer client.Close()

	remoteDir := fmt.Sprintf("%s/%s", remoteAppsDir, app.Name)

	previousVersion := d.readCurrentVersion(client, remoteDir)

	if err := client.UploadFile(composePath, remoteDir+"/compose.yml"); err != nil {
		return fmt.Errorf("failed to upload compose.yml: %w", err)
	}

	if err := client.WriteFile(remoteDir+"/docker-compose.override.yml", override); err != nil {
		return fmt.Errorf("failed to write override: %w", err)
	}

	if d.Repo.Cluster.Registry.Username != "" {
		loginCmd := fmt.Sprintf("echo '%s' | docker login %s -u %s --password-stdin",
			d.Repo.Cluster.Registry.Password,
			d.Repo.Cluster.Registry.Server,
			d.Repo.Cluster.Registry.Username)
		if _, _, err := client.RunCommand(loginCmd); err != nil {
			d.log.Warnf("Docker login failed: %v", err)
		}
	}

	pullCmd := fmt.Sprintf("cd %s && docker compose -f compose.yml -f docker-compose.override.yml pull", remoteDir)
	d.log.Info("Pulling images...")
	if _, stderr, err := client.RunCommand(pullCmd); err != nil {
		return fmt.Errorf("pull failed: %w\n%s", err, stderr)
	}

	upCmd := d.composeUpCommand(app)
	fullCmd := fmt.Sprintf("cd %s && %s", remoteDir, upCmd)
	d.log.Info("Starting containers...")
	if _, stderr, err := client.RunCommand(fullCmd); err != nil {
		return fmt.Errorf("deploy failed: %w\n%s", err, stderr)
	}

	state := &DeployState{
		App:             app.Name,
		CurrentVersion:  app.Version,
		PreviousVersion: previousVersion,
		DeployedAt:      time.Now(),
	}
	stateJSON, err := state.Marshal()
	if err != nil {
		d.log.Warnf("Failed to marshal deploy state: %v", err)
	} else if err := client.WriteFile(remoteDir+"/state.json", stateJSON); err != nil {
		d.log.Warnf("Failed to save deploy state: %v", err)
	}

	d.log.Infof("Node %s: deployed %s:%s", node.ID, app.Name, app.Version)
	return nil
}

// Rollback re-deploys the previous version of an app.
func (d *Deployer) Rollback(appName string) error {
	app := d.Repo.GetApp(appName)
	if app == nil {
		return fmt.Errorf("app '%s' not found", appName)
	}

	targetNodes := app.GetTargetNodes(d.Repo.Cluster.Nodes)

	for _, nodeID := range targetNodes {
		node := d.Repo.Cluster.GetNode(nodeID)
		if node == nil {
			continue
		}

		client, err := d.connect(node)
		if err != nil {
			return err
		}

		remoteDir := fmt.Sprintf("%s/%s", remoteAppsDir, appName)
		previousVersion := d.readPreviousVersion(client, remoteDir)
		client.Close()

		if previousVersion == "" {
			return fmt.Errorf("no previous version found for %s on node %s", appName, nodeID)
		}

		d.log.Infof("Rolling back %s to %s on node %s", appName, previousVersion, nodeID)
	}

	node := d.Repo.Cluster.GetNode(targetNodes[0])
	client, err := d.connect(node)
	if err != nil {
		return err
	}
	remoteDir := fmt.Sprintf("%s/%s", remoteAppsDir, appName)
	previousVersion := d.readPreviousVersion(client, remoteDir)
	client.Close()

	if previousVersion == "" {
		return fmt.Errorf("no previous version found for %s", appName)
	}

	return d.Deploy(appName, previousVersion)
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

		remoteDir := fmt.Sprintf("%s/%s", remoteAppsDir, appName)
		psCmd := fmt.Sprintf("cd %s && docker compose -f compose.yml -f docker-compose.override.yml ps --format '{{.Name}}\t{{.Status}}' 2>/dev/null || echo 'NOT_DEPLOYED'", remoteDir)
		stdout, _, err := client.RunCommand(psCmd)
		client.Close()

		status := NodeStatus{NodeID: nodeID}
		if err != nil || strings.TrimSpace(stdout) == "NOT_DEPLOYED" {
			status.State = "not deployed"
		} else {
			status.State = "running"
			status.Containers = strings.TrimSpace(stdout)
		}

		version := d.readCurrentVersion(client, remoteDir)
		status.Version = version

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
	// Containers is the docker compose ps output.
	Containers string
	// Error is set if the node could not be reached.
	Error string
}

func (d *Deployer) connect(node *config.NodeConfig) (*ssh.Client, error) {
	user := os.Getenv("USER")
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

func (d *Deployer) composeUpCommand(app *config.AppConfig) string {
	base := "docker compose -f compose.yml -f docker-compose.override.yml up -d"
	if app.SecretsPrefix != "" {
		return fmt.Sprintf("secretsauce run %s -- %s", app.SecretsPrefix, base)
	}
	return base
}

func (d *Deployer) readCurrentVersion(client *ssh.Client, remoteDir string) string {
	stdout, _, err := client.RunCommand(fmt.Sprintf("cat %s/state.json 2>/dev/null || echo '{}'", remoteDir))
	if err != nil {
		return ""
	}
	state, err := UnmarshalState(strings.TrimSpace(stdout))
	if err != nil {
		return ""
	}
	return state.CurrentVersion
}

func (d *Deployer) readPreviousVersion(client *ssh.Client, remoteDir string) string {
	stdout, _, err := client.RunCommand(fmt.Sprintf("cat %s/state.json 2>/dev/null || echo '{}'", remoteDir))
	if err != nil {
		return ""
	}
	state, err := UnmarshalState(strings.TrimSpace(stdout))
	if err != nil {
		return ""
	}
	return state.PreviousVersion
}
