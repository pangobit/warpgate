package deploy

import (
	"fmt"
	"strings"
)

// LogsOptions configures the logs command.
type LogsOptions struct {
	// NodeID is the target node identifier.
	NodeID string
	// App filters logs to containers matching this app name.
	App string
	// Tail is the number of recent lines to show per container.
	Tail int
	// Grep filters log output server-side with grep.
	Grep string
}

// BuildLogsCommand constructs the remote shell command for fetching container logs.
func BuildLogsCommand(opts LogsOptions) string {
	tail := opts.Tail
	if tail <= 0 {
		tail = 100
	}

	dockerPS := "docker ps --format '{{.Names}}'"
	if opts.App != "" {
		dockerPS += " --filter name=" + shellQuote(opts.App)
	}

	logsCmd := fmt.Sprintf(
		`%s | xargs -I {} sh -c 'docker logs --tail %d {} 2>&1 | sed "s/^/[{}] /"'`,
		dockerPS, tail,
	)

	if opts.Grep != "" {
		logsCmd += " | grep " + shellQuote(opts.Grep)
	}

	return logsCmd
}

// Logs fetches recent container logs from a node.
func (d *Deployer) Logs(opts LogsOptions) error {
	node := d.Repo.Cluster.GetNode(opts.NodeID)
	if node == nil {
		return fmt.Errorf("node '%s' not found in cluster config", opts.NodeID)
	}

	client, err := d.connect(node)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", opts.NodeID, err)
	}
	defer client.Close()

	cmd := BuildLogsCommand(opts)
	stdout, stderr, err := client.RunCommand(cmd)
	if err != nil {
		// grep returns exit code 1 when no matches found — not an error
		if opts.Grep != "" && strings.TrimSpace(stdout) == "" {
			fmt.Println("No log lines matched the grep pattern.")
			return nil
		}
		return fmt.Errorf("failed to fetch logs: %w\n%s", err, stderr)
	}

	if strings.TrimSpace(stdout) == "" {
		fmt.Println("No containers found.")
		return nil
	}

	fmt.Print(stdout)
	return nil
}
