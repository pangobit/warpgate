package deploy

import (
	"fmt"
	"os"
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

// LogsResult is the output from fetching container logs.
type LogsResult struct {
	// Output is the raw prefixed log output.
	Output string
	// Message describes an empty result.
	Message string
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
	result, err := d.FetchLogs(opts)
	if err != nil {
		return err
	}
	if result.Message != "" {
		_, err := fmt.Fprintln(os.Stdout, result.Message)
		return err
	}
	_, err = fmt.Fprint(os.Stdout, result.Output)
	return err
}

// FetchLogs fetches recent container logs from a node.
func (d *Deployer) FetchLogs(opts LogsOptions) (LogsResult, error) {
	node := d.Repo.Cluster.GetNode(opts.NodeID)
	if node == nil {
		return LogsResult{}, fmt.Errorf("node '%s' not found in cluster config", opts.NodeID)
	}

	client, err := d.connect(node)
	if err != nil {
		return LogsResult{}, fmt.Errorf("failed to connect to %s: %w", opts.NodeID, err)
	}
	defer client.Close()

	cmd := BuildLogsCommand(opts)
	stdout, stderr, err := client.RunCommand(cmd)
	if err != nil {
		if opts.Grep != "" && strings.TrimSpace(stdout) == "" {
			return LogsResult{Message: "No log lines matched the grep pattern."}, nil
		}
		return LogsResult{}, fmt.Errorf("failed to fetch logs: %w\n%s", err, stderr)
	}

	if strings.TrimSpace(stdout) == "" {
		return LogsResult{Message: "No containers found."}, nil
	}

	return LogsResult{Output: stdout}, nil
}
