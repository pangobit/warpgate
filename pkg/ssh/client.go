// Package ssh provides SSH client functionality for remote node operations.
package ssh

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Client wraps an SSH connection to a remote node.
type Client struct {
	// Host is the remote hostname or IP.
	Host string
	// Port is the SSH port number.
	Port int
	// User is the SSH username.
	User string
	// PrivateKey is the path to the SSH private key file (empty for Tailscale SSH).
	PrivateKey string
	// TailscaleSSH uses the ssh binary (Tailscale handles auth) instead of key-based auth.
	TailscaleSSH bool

	client *gossh.Client
}

// NewClient creates a new SSH client configuration.
func NewClient(host, user, privateKeyPath string) (*Client, error) {
	return &Client{
		Host:       host,
		Port:       22,
		User:       user,
		PrivateKey: privateKeyPath,
	}, nil
}

// NewTailscaleClient creates an SSH client that uses Tailscale SSH (no keys needed).
func NewTailscaleClient(host, user string) *Client {
	return &Client{
		Host:         host,
		Port:         22,
		User:         user,
		TailscaleSSH: true,
	}
}

// Connect establishes an SSH connection to the remote host.
// For Tailscale SSH mode, this is a no-op since we shell out per command.
func (c *Client) Connect() error {
	if c.TailscaleSSH {
		return nil
	}

	key, err := os.ReadFile(c.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to read private key: %w", err)
	}

	signer, err := gossh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	hostKeyCallback, err := c.getHostKeyCallback()
	if err != nil {
		hostKeyCallback = gossh.InsecureIgnoreHostKey()
	}

	config := &gossh.ClientConfig{
		User: c.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	client, err := gossh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	c.client = client
	return nil
}

// Close closes the SSH connection.
func (c *Client) Close() error {
	if c.TailscaleSSH {
		return nil
	}
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// RunCommand executes a command on the remote host and returns stdout, stderr, and any error.
func (c *Client) RunCommand(cmd string) (string, string, error) {
	if c.TailscaleSSH {
		return c.runCommandViaBinary(cmd)
	}

	if c.client == nil {
		return "", "", fmt.Errorf("not connected")
	}

	session, err := c.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return "", "", err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return "", "", err
	}

	if err := session.Start(cmd); err != nil {
		return "", "", fmt.Errorf("failed to start command: %w", err)
	}

	var stdoutBuf strings.Builder
	var stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			stdoutBuf.WriteString(scanner.Text())
			stdoutBuf.WriteString("\n")
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteString("\n")
		}
	}()

	wg.Wait()
	err = session.Wait()

	return stdoutBuf.String(), stderrBuf.String(), err
}

func (c *Client) runCommandViaBinary(cmd string) (string, string, error) {
	target := fmt.Sprintf("%s@%s", c.User, c.Host)
	sshCmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new", target, cmd)
	var stdoutBuf, stderrBuf strings.Builder
	sshCmd.Stdout = &stdoutBuf
	sshCmd.Stderr = &stderrBuf
	err := sshCmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		if errMsg != "" {
			return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("%w: %s", err, errMsg)
		}
	}
	return stdoutBuf.String(), stderrBuf.String(), err
}

// RunScript pipes a script to bash on the remote host and executes it.
func (c *Client) RunScript(script string) error {
	if c.TailscaleSSH {
		return c.runScriptViaBinary(script)
	}

	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	session.Stdin = strings.NewReader(script)

	output, err := session.CombinedOutput("bash -s")
	if err != nil {
		return fmt.Errorf("script failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Println(string(output))
	return nil
}

func (c *Client) runScriptViaBinary(script string) error {
	sshCmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new", fmt.Sprintf("%s@%s", c.User, c.Host), "bash -s")
	sshCmd.Stdin = strings.NewReader(script)
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr
	if err := sshCmd.Run(); err != nil {
		return fmt.Errorf("script failed: %w", err)
	}
	return nil
}

// WriteFile writes content to a file on the remote host via stdin piping.
func (c *Client) WriteFile(remotePath, content string) error {
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s", filepath.Dir(remotePath), remotePath)

	if c.TailscaleSSH {
		sshCmd := exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new", fmt.Sprintf("%s@%s", c.User, c.Host), cmd)
		sshCmd.Stdin = strings.NewReader(content)
		if output, err := sshCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("write failed: %w\n%s", err, string(output))
		}
		return nil
	}

	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdin = strings.NewReader(content)

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("write failed: %w\n%s", err, string(output))
	}

	return nil
}

// UploadFile copies a local file to the remote host.
func (c *Client) UploadFile(localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}

	return c.WriteFile(remotePath, string(data))
}

func (c *Client) getHostKeyCallback() (gossh.HostKeyCallback, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	knownHostsPath := filepath.Join(homeDir, ".ssh", "known_hosts")

	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("known_hosts not found")
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read known_hosts: %w", err)
	}

	return callback, nil
}
