package bootstrap

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/pangobit/warpgate/pkg/compose"
	"github.com/pangobit/warpgate/pkg/config"
	"github.com/pangobit/warpgate/pkg/ssh"
)

const passwordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const passwordLength = 24

// generatePassword creates a cryptographically random password.
func generatePassword() (string, error) {
	b := make([]byte, passwordLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordCharset))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random password: %w", err)
		}
		b[i] = passwordCharset[n.Int64()]
	}
	return string(b), nil
}

const goVersion = "1.26.1"

// run executes a command on the remote host, returning a combined error with stderr.
func run(client *ssh.Client, cmd string) error {
	_, stderr, err := client.RunCommand(cmd)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// commandExists checks whether a command is available on the remote host.
func commandExists(client *ssh.Client, name string) bool {
	_, _, err := client.RunCommand("command -v " + name)
	return err == nil
}

// createUser creates the warpgate system user if it doesn't already exist.
func createUser(client *ssh.Client) (string, error) {
	if _, _, err := client.RunCommand("id -u warpgate"); err == nil {
		return "already exists", nil
	}

	if err := run(client, "sudo useradd -m -s /bin/bash warpgate"); err != nil {
		return "", fmt.Errorf("useradd failed: %w", err)
	}

	run(client, "sudo touch /home/warpgate/.bashrc && sudo chown warpgate:warpgate /home/warpgate/.bashrc")
	return "", nil
}

// installGo downloads and installs the Go toolchain.
func installGo(client *ssh.Client, osInfo *OSInfo) (string, error) {
	stdout, _, _ := client.RunCommand("/usr/local/go/bin/go version")
	if strings.Contains(stdout, goVersion) {
		return "already installed", nil
	}

	arch := osInfo.goArch()
	tarball := fmt.Sprintf("go%s.linux-%s.tar.gz", goVersion, arch)
	url := "https://go.dev/dl/" + tarball

	if err := run(client, fmt.Sprintf("curl -fsSL %s -o /tmp/%s", url, tarball)); err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	if err := run(client, "sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/"+tarball); err != nil {
		return "", fmt.Errorf("extract failed: %w", err)
	}

	run(client, "rm -f /tmp/"+tarball)

	run(client, `grep -q '/usr/local/go/bin' /home/warpgate/.bashrc 2>/dev/null || echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee -a /home/warpgate/.bashrc`)

	run(client, "sudo -u warpgate mkdir -p /home/warpgate/go/bin /home/warpgate/go/src /home/warpgate/go/pkg")

	return "", nil
}

// installDocker installs Docker and the Compose plugin, branching by OS family.
func installDocker(client *ssh.Client, osInfo *OSInfo) (string, error) {
	if commandExists(client, "docker") {
		if err := run(client, "sudo systemctl enable docker && sudo systemctl start docker"); err != nil {
			return "", fmt.Errorf("failed to enable docker: %w", err)
		}
		return "already installed", nil
	}

	if osInfo.IsDebianBased() {
		return installDockerDebian(client)
	}
	if osInfo.IsRHELBased() {
		return installDockerRHEL(client)
	}
	return installDockerGeneric(client)
}

func installDockerDebian(client *ssh.Client) (string, error) {
	cmds := []string{
		"sudo apt-get update -qq",
		"sudo apt-get install -y ca-certificates curl gnupg lsb-release",
		"sudo install -m 0755 -d /etc/apt/keyrings",
	}
	for _, cmd := range cmds {
		if err := run(client, cmd); err != nil {
			return "", fmt.Errorf("prerequisite install failed: %w", err)
		}
	}

	run(client, "curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg 2>/dev/null || curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg 2>/dev/null")
	run(client, "sudo chmod a+r /etc/apt/keyrings/docker.gpg")

	repoCmd := `DOCKER_DIST=$(. /etc/os-release && echo "$ID") && ` +
		`DOCKER_CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME") && ` +
		`DOCKER_ARCH=$(dpkg --print-architecture) && ` +
		`echo "deb [arch=${DOCKER_ARCH} signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${DOCKER_DIST} ${DOCKER_CODENAME} stable" | ` +
		`sudo tee /etc/apt/sources.list.d/docker.list > /dev/null`
	run(client, repoCmd)

	if err := run(client, "sudo apt-get update -qq"); err != nil {
		return "", fmt.Errorf("apt update failed: %w", err)
	}

	if err := run(client, "sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"); err != nil {
		return "", fmt.Errorf("docker install failed: %w", err)
	}

	run(client, "sudo systemctl enable docker && sudo systemctl start docker")
	return "", nil
}

func installDockerRHEL(client *ssh.Client) (string, error) {
	run(client, "sudo yum remove -y docker docker-client docker-client-latest docker-common docker-latest docker-latest-logrotate docker-logrotate docker-engine 2>/dev/null || true")

	if err := run(client, "sudo yum install -y yum-utils"); err != nil {
		return "", fmt.Errorf("yum-utils install failed: %w", err)
	}

	run(client, "sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo 2>/dev/null || "+
		"sudo yum-config-manager --add-repo https://download.docker.com/linux/rhel/docker-ce.repo 2>/dev/null || "+
		"sudo yum-config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo 2>/dev/null || true")

	if err := run(client, "sudo yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"); err != nil {
		return "", fmt.Errorf("docker install failed: %w", err)
	}

	run(client, "sudo systemctl enable docker && sudo systemctl start docker")
	return "", nil
}

func installDockerGeneric(client *ssh.Client) (string, error) {
	if err := run(client, "curl -fsSL https://get.docker.com -o /tmp/get-docker.sh && sudo sh /tmp/get-docker.sh && rm -f /tmp/get-docker.sh"); err != nil {
		return "", fmt.Errorf("docker install failed: %w", err)
	}

	run(client, "sudo systemctl enable docker || true")
	run(client, "sudo systemctl start docker || true")
	return "", nil
}

// addDockerGroup adds the warpgate user to the docker group.
func addDockerGroup(client *ssh.Client) (string, error) {
	if err := run(client, "sudo usermod -aG docker warpgate"); err != nil {
		return "", fmt.Errorf("failed to add warpgate to docker group: %w", err)
	}
	return "", nil
}

// installSecretSauce installs the SecretSauce binary via the private Go proxy.
func installSecretSauce(client *ssh.Client, goProxy string) (string, error) {
	if goProxy == "" {
		return "skipped (no go_proxy)", nil
	}

	stdout, _, _ := client.RunCommand("test -f /home/warpgate/go/bin/secretsauce && echo exists")
	if strings.Contains(stdout, "exists") {
		return "already installed", nil
	}

	installCmd := fmt.Sprintf(
		`sudo -u warpgate bash -c 'cd /home/warpgate && export GOPROXY="%s,https://proxy.golang.org,direct" && export GONOSUMDB="github.com/pangobit/*" && export GOPATH="/home/warpgate/go" && export PATH="/usr/local/go/bin:$GOPATH/bin:$PATH" && mkdir -p "$GOPATH/bin" && go install github.com/pangobit/secretsauce@latest'`,
		goProxy)

	if err := run(client, installCmd); err != nil {
		return "", fmt.Errorf("go install failed: %w", err)
	}

	run(client, fmt.Sprintf(`grep -q 'GOPROXY' /home/warpgate/.bashrc 2>/dev/null || { echo 'export GOPROXY="%s,https://proxy.golang.org,direct"' | sudo tee -a /home/warpgate/.bashrc && echo 'export GONOSUMDB="github.com/pangobit/*"' | sudo tee -a /home/warpgate/.bashrc && echo 'export GOPATH="/home/warpgate/go"' | sudo tee -a /home/warpgate/.bashrc && echo 'export PATH="/usr/local/go/bin:$GOPATH/bin:$PATH"' | sudo tee -a /home/warpgate/.bashrc; }`, goProxy))

	run(client, "sudo ln -sf /home/warpgate/go/bin/secretsauce /usr/local/bin/secretsauce 2>/dev/null || true")

	return "", nil
}

// setupSSHKeys creates SSH keys for the warpgate user.
func setupSSHKeys(client *ssh.Client) (string, error) {
	if err := run(client, "sudo -u warpgate mkdir -p /home/warpgate/.ssh && sudo chmod 700 /home/warpgate/.ssh"); err != nil {
		return "", fmt.Errorf("failed to create .ssh directory: %w", err)
	}

	stdout, _, _ := client.RunCommand("test -f /home/warpgate/.ssh/authorized_keys && echo exists")
	if !strings.Contains(stdout, "exists") {
		run(client, "sudo -u warpgate touch /home/warpgate/.ssh/authorized_keys && sudo chmod 600 /home/warpgate/.ssh/authorized_keys")
	}

	stdout, _, _ = client.RunCommand("test -f /home/warpgate/.ssh/id_ed25519 && echo exists")
	if !strings.Contains(stdout, "exists") {
		if err := run(client, `sudo -u warpgate ssh-keygen -t ed25519 -C "warpgate@$(hostname)" -N "" -f /home/warpgate/.ssh/id_ed25519`); err != nil {
			return "", fmt.Errorf("ssh-keygen failed: %w", err)
		}
	}

	pubkey, _, _ := client.RunCommand("sudo cat /home/warpgate/.ssh/id_ed25519.pub")
	return strings.TrimSpace(pubkey), nil
}

// setupWarpgate creates the /opt/warpgate directory structure, Docker network, and Traefik.
func setupWarpgate(client *ssh.Client, networking *config.NetworkingConfig) (string, error) {
	if err := run(client, "sudo mkdir -p /opt/warpgate/apps /opt/warpgate/traefik/dynamic && sudo chown -R warpgate:warpgate /opt/warpgate"); err != nil {
		return "", fmt.Errorf("mkdir failed: %w", err)
	}

	run(client, "docker network create warpgate 2>/dev/null || true")

	if networking != nil && len(networking.Traefik.EntryPoints) > 0 {
		traefikYAML, err := compose.GenerateTraefikCompose(networking)
		if err != nil {
			return "", fmt.Errorf("failed to generate traefik compose: %w", err)
		}

		if err := client.WriteFile("/opt/warpgate/traefik/compose.yml", traefikYAML); err != nil {
			return "", fmt.Errorf("failed to write traefik compose: %w", err)
		}

		if err := run(client, "cd /opt/warpgate/traefik && docker compose up -d"); err != nil {
			return "", fmt.Errorf("traefik start failed: %w", err)
		}
	}

	return "", nil
}

// setupSecretsServer installs a systemd service for SecretSauce with auto-unseal.
// If masterPassword is provided, it also initializes the vault and generates the master key file.
func setupSecretsServer(client *ssh.Client, masterPassword string) (string, error) {
	if err := run(client, "sudo mkdir -p /opt/warpgate/secretsauce && sudo chown warpgate:warpgate /opt/warpgate/secretsauce"); err != nil {
		return "", fmt.Errorf("mkdir failed: %w", err)
	}

	unit := `[Unit]
Description=SecretSauce Secrets Server
After=network.target

[Service]
Type=simple
User=warpgate
ExecStart=/usr/local/bin/secretsauce server --db-path /opt/warpgate/secretsauce/vault.db --http-port 8090 --grpc-port 8091 --web-ui true --identity none
Environment=SS_MASTER_KEY_FILE=/opt/warpgate/secretsauce/master.key
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target`

	if err := client.WriteFile("/tmp/secretsauce.service", unit); err != nil {
		return "", fmt.Errorf("failed to write unit file: %w", err)
	}

	if err := run(client, "sudo mv /tmp/secretsauce.service /etc/systemd/system/secretsauce.service && sudo systemctl daemon-reload && sudo systemctl enable secretsauce"); err != nil {
		return "", fmt.Errorf("failed to install service: %w", err)
	}

	generated := false
	if masterPassword == "" {
		pwd, err := generatePassword()
		if err != nil {
			return "", fmt.Errorf("failed to generate password: %w", err)
		}
		masterPassword = pwd
		generated = true
	}

	initCmd := fmt.Sprintf(
		`sudo -u warpgate bash -c 'cd /home/warpgate && secretsauce init --db-path /opt/warpgate/secretsauce/vault.db --password "%s" --output-key'`,
		strings.ReplaceAll(masterPassword, `"`, `\"`))

	stdout, _, err := client.RunCommand(initCmd)
	if err != nil {
		return "", fmt.Errorf("vault init failed: %w", err)
	}

	masterKey := strings.TrimSpace(stdout)
	if err := client.WriteFile("/opt/warpgate/secretsauce/master.key", masterKey+"\n"); err != nil {
		return "", fmt.Errorf("failed to write master key: %w", err)
	}

	run(client, "sudo chown warpgate:warpgate /opt/warpgate/secretsauce/master.key && sudo chmod 600 /opt/warpgate/secretsauce/master.key")

	if err := run(client, "sudo systemctl start secretsauce"); err != nil {
		return "", fmt.Errorf("failed to start service: %w", err)
	}

	if generated {
		return "password:" + masterPassword, nil
	}
	return "initialized and started", nil
}

func (o *OSInfo) goArch() string {
	switch o.Arch {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "arm", "armv7l":
		return "armv6l"
	case "386", "i386":
		return "386"
	default:
		return "amd64"
	}
}
