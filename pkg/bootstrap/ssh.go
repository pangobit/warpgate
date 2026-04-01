package bootstrap

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHClient wraps an SSH connection to a remote node.
type SSHClient struct {
	// Host is the remote hostname or IP.
	Host string
	// Port is the SSH port number.
	Port int
	// User is the SSH username.
	User string
	// PrivateKey is the path to the SSH private key file.
	PrivateKey string

	client *ssh.Client
}

// NewSSHClient creates a new SSH client configuration.
func NewSSHClient(host, user, privateKeyPath string) (*SSHClient, error) {
	client := &SSHClient{
		Host:       host,
		Port:       22,
		User:       user,
		PrivateKey: privateKeyPath,
	}

	return client, nil
}

// Connect establishes an SSH connection to the remote host.
func (c *SSHClient) Connect() error {
	key, err := os.ReadFile(c.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	hostKeyCallback, err := c.getHostKeyCallback()
	if err != nil {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	config := &ssh.ClientConfig{
		User: c.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	c.client = client
	return nil
}

// Close closes the SSH connection.
func (c *SSHClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// RunCommand executes a command on the remote host and returns stdout, stderr, and any error.
func (c *SSHClient) RunCommand(cmd string) (string, string, error) {
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

// RunScript pipes a script to bash on the remote host and executes it.
func (c *SSHClient) RunScript(script string) error {
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

// DetectOS runs OS detection commands on the remote host and returns the parsed result.
func (c *SSHClient) DetectOS() (*OSInfo, error) {
	arch, _, err := c.RunCommand("uname -m")
	if err != nil {
		return nil, fmt.Errorf("failed to detect arch: %w", err)
	}
	arch = strings.TrimSpace(arch)

	stdout, _, err := c.RunCommand("cat /etc/os-release 2>/dev/null || echo 'NOT_FOUND'")
	if err != nil || strings.TrimSpace(stdout) == "NOT_FOUND" {
		stdout, _, _ = c.RunCommand("cat /etc/lsb-release 2>/dev/null || echo 'NOT_FOUND'")
	}

	return DetectOSFromOutput(stdout, arch), nil
}

// UploadFile copies a local file to the remote host via stdin piping.
func (c *SSHClient) UploadFile(localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}

	cmd := fmt.Sprintf("mkdir -p %s && cat > %s", filepath.Dir(remotePath), remotePath)
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdin = strings.NewReader(string(data))

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("upload failed: %w\n%s", err, string(output))
	}

	return nil
}

func (c *SSHClient) getHostKeyCallback() (ssh.HostKeyCallback, error) {
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

// AddToKnownHosts adds the remote host's key to the local known_hosts file.
func (c *SSHClient) AddToKnownHosts() error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	fmt.Printf("Host key for %s added to known_hosts\n", addr)

	return nil
}
