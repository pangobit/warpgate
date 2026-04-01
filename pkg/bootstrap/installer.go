package bootstrap

import (
	"fmt"
	"strings"

	"github.com/pangobit/warpgate/pkg/compose"
	"github.com/pangobit/warpgate/pkg/config"
)

const goVersion = "1.26.1"

// InstallScript generates the installation script for the detected OS.
// goProxy is the URL of a private Go module proxy for installing private packages.
// networking is the cluster networking config used to generate the Traefik compose file.
func (o *OSInfo) InstallScript(goProxy string, networking *config.NetworkingConfig) string {
	var parts []string

	parts = append(parts, "#!/bin/bash")
	parts = append(parts, "set -e")
	parts = append(parts, "")
	parts = append(parts, "# Warpgate Node Bootstrap Script")
	parts = append(parts, fmt.Sprintf("# OS: %s %s", o.Distro, o.Version))
	parts = append(parts, fmt.Sprintf("# Arch: %s", o.Arch))
	parts = append(parts, "")

	parts = append(parts, o.userScript())
	parts = append(parts, o.goScript())
	parts = append(parts, o.dockerScript())
	parts = append(parts, o.postDockerUserScript())
	parts = append(parts, o.secretSauceScript(goProxy))
	parts = append(parts, o.sshScript())
	parts = append(parts, o.warpgateSetupScript(networking))

	parts = append(parts, "")
	parts = append(parts, "echo 'Bootstrap complete!'")
	parts = append(parts, "echo 'Node is ready for Warpgate deployments'")

	return strings.Join(parts, "\n")
}

func (o *OSInfo) userScript() string {
	return `echo "Creating warpgate user..."
if ! id -u warpgate &>/dev/null; then
    sudo useradd -m -s /bin/bash warpgate
    echo "warpgate user created"
else
    echo "warpgate user already exists"
fi

if [ ! -f /home/warpgate/.bashrc ]; then
    sudo touch /home/warpgate/.bashrc
    sudo chown warpgate:warpgate /home/warpgate/.bashrc
fi`
}

func (o *OSInfo) postDockerUserScript() string {
	return `echo "Adding warpgate user to docker group..."
sudo usermod -aG docker warpgate`
}

func (o *OSInfo) goScript() string {
	arch := o.goArch()

	return fmt.Sprintf(`echo "Installing Go %s..."

# Download and install Go
GO_VERSION="%s"
GO_ARCH="%s"
GO_TARBALL="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"

if ! command -v go &>/dev/null || ! go version | grep -q "${GO_VERSION}"; then
    cd /tmp
    echo "Downloading Go..."
    curl -fsSL "${GO_URL}" -o "${GO_TARBALL}"

    echo "Installing Go..."
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "${GO_TARBALL}"
    rm "${GO_TARBALL}"

    # Add to warpgate user PATH
    if ! grep -q '/usr/local/go/bin' /home/warpgate/.bashrc 2>/dev/null; then
        echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee -a /home/warpgate/.bashrc
    fi

    echo "Go ${GO_VERSION} installed successfully"
else
    echo "Go ${GO_VERSION} already installed"
fi

# Create Go workspace for warpgate
sudo -u warpgate mkdir -p /home/warpgate/go/{bin,src,pkg}`, goVersion, goVersion, arch)
}

func (o *OSInfo) dockerScript() string {
	if o.IsDebianBased() {
		return o.dockerDebianScript()
	}
	if o.IsRHELBased() {
		return o.dockerRHELScript()
	}
	return o.dockerGenericScript()
}

func (o *OSInfo) dockerDebianScript() string {
	return `echo "Installing Docker..."

# Setup Docker registry authentication if credentials are provided
setup_registry_auth() {
    if [ -n "${REGISTRY_USERNAME}" ] && [ -n "${REGISTRY_TOKEN}" ] && [ -n "${REGISTRY_SERVER}" ]; then
        echo "Configuring Docker registry authentication..."
        sudo -u warpgate mkdir -p /home/warpgate/.docker
        echo "${REGISTRY_TOKEN}" | docker login "${REGISTRY_SERVER}" -u "${REGISTRY_USERNAME}" --password-stdin

        # Copy config to warpgate user
        if [ -f ~/.docker/config.json ]; then
            cp ~/.docker/config.json /home/warpgate/.docker/
            chown warpgate:warpgate /home/warpgate/.docker/config.json
            chmod 600 /home/warpgate/.docker/config.json
        fi
        echo "Registry authentication configured"
    fi
}

if ! command -v docker &>/dev/null; then
    # Install prerequisites
    sudo apt-get update
    sudo apt-get install -y ca-certificates curl gnupg lsb-release

    # Add Docker's official GPG key
    sudo install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg || \
    curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg

    # Add repository
    DOCKER_DIST=$(. /etc/os-release && echo "$ID")
    DOCKER_CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
    DOCKER_ARCH=$(dpkg --print-architecture)
    echo "deb [arch=${DOCKER_ARCH} signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${DOCKER_DIST} ${DOCKER_CODENAME} stable" | \
      sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

    # Install Docker
    sudo apt-get update
    sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    echo "Docker installed successfully"
else
    echo "Docker already installed"
fi

# Enable and start Docker
sudo systemctl enable docker
sudo systemctl start docker

# Setup registry auth if credentials are provided
setup_registry_auth`
}

func (o *OSInfo) dockerRHELScript() string {
	return `echo "Installing Docker..."

# Setup Docker registry authentication if credentials are provided
setup_registry_auth() {
    if [ -n "${REGISTRY_USERNAME}" ] && [ -n "${REGISTRY_TOKEN}" ] && [ -n "${REGISTRY_SERVER}" ]; then
        echo "Configuring Docker registry authentication..."
        sudo -u warpgate mkdir -p /home/warpgate/.docker
        echo "${REGISTRY_TOKEN}" | docker login "${REGISTRY_SERVER}" -u "${REGISTRY_USERNAME}" --password-stdin

        # Copy config to warpgate user
        if [ -f ~/.docker/config.json ]; then
            cp ~/.docker/config.json /home/warpgate/.docker/
            chown warpgate:warpgate /home/warpgate/.docker/config.json
            chmod 600 /home/warpgate/.docker/config.json
        fi
        echo "Registry authentication configured"
    fi
}

if ! command -v docker &>/dev/null; then
    # Remove old versions
    sudo yum remove -y docker docker-client docker-client-latest docker-common docker-latest \
        docker-latest-logrotate docker-logrotate docker-engine 2>/dev/null || true

    # Install yum-utils
    sudo yum install -y yum-utils

    # Add repository
    sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo || \
    sudo yum-config-manager --add-repo https://download.docker.com/linux/rhel/docker-ce.repo || \
    sudo yum-config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo

    # Install Docker
    sudo yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    echo "Docker installed successfully"
else
    echo "Docker already installed"
fi

# Enable and start Docker
sudo systemctl enable docker
sudo systemctl start docker

# Setup registry auth if credentials are provided
setup_registry_auth`
}

func (o *OSInfo) dockerGenericScript() string {
	return `echo "Installing Docker via convenience script..."

if ! command -v docker &>/dev/null; then
    # Use Docker's convenience script
    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
    sudo sh /tmp/get-docker.sh
    rm /tmp/get-docker.sh

    echo "Docker installed successfully"
else
    echo "Docker already installed"
fi

# Enable and start Docker
sudo systemctl enable docker || true
sudo systemctl start docker || true`
}

func (o *OSInfo) sshScript() string {
	return `
echo "Setting up SSH keys..."

# Create .ssh directory for warpgate user
sudo -u warpgate mkdir -p /home/warpgate/.ssh
sudo chmod 700 /home/warpgate/.ssh

# Add authorized_keys placeholder (user should provide their keys)
if [ ! -f /home/warpgate/.ssh/authorized_keys ]; then
    sudo -u warpgate touch /home/warpgate/.ssh/authorized_keys
    sudo chmod 600 /home/warpgate/.ssh/authorized_keys
    echo "Created authorized_keys file - add your SSH public key to enable passwordless login"
fi

# Generate SSH key for warpgate user if not exists
if [ ! -f /home/warpgate/.ssh/id_ed25519 ]; then
    sudo -u warpgate ssh-keygen -t ed25519 -C "warpgate@$(hostname)" -N "" -f /home/warpgate/.ssh/id_ed25519
    echo "Generated SSH key for warpgate user"
fi

echo "SSH setup complete"
echo "Public key:"
sudo cat /home/warpgate/.ssh/id_ed25519.pub`
}

func (o *OSInfo) secretSauceScript(goProxy string) string {
	if goProxy == "" {
		return `echo "Skipping SecretSauce installation (no go_proxy configured)"`
	}

	return fmt.Sprintf(`echo "Installing SecretSauce..."

if [ ! -f /home/warpgate/go/bin/secretsauce ]; then
    sudo -u warpgate bash -c '\
        export GOPROXY="%s,https://proxy.golang.org,direct" && \
        export GONOSUMDB="github.com/pangobit/*" && \
        export GOPATH="/home/warpgate/go" && \
        export PATH="/usr/local/go/bin:$GOPATH/bin:$PATH" && \
        mkdir -p "$GOPATH/bin" && \
        go install github.com/pangobit/secretsauce@latest'
    echo "SecretSauce installed successfully"
else
    echo "SecretSauce already installed"
fi

if ! grep -q 'GOPROXY' /home/warpgate/.bashrc 2>/dev/null; then
    echo 'export GOPROXY="%s,https://proxy.golang.org,direct"' | sudo tee -a /home/warpgate/.bashrc
    echo 'export GONOSUMDB="github.com/pangobit/*"' | sudo tee -a /home/warpgate/.bashrc
    echo 'export GOPATH="/home/warpgate/go"' | sudo tee -a /home/warpgate/.bashrc
    echo 'export PATH="/usr/local/go/bin:$GOPATH/bin:$PATH"' | sudo tee -a /home/warpgate/.bashrc
fi

sudo ln -sf /home/warpgate/go/bin/secretsauce /usr/local/bin/secretsauce 2>/dev/null || true`, goProxy, goProxy)
}

func (o *OSInfo) warpgateSetupScript(networking *config.NetworkingConfig) string {
	var parts []string

	parts = append(parts, `
echo "Setting up Warpgate directories..."
sudo mkdir -p /opt/warpgate/apps
sudo mkdir -p /opt/warpgate/traefik
sudo chown -R warpgate:warpgate /opt/warpgate`)

	parts = append(parts, `
echo "Creating warpgate Docker network..."
docker network create warpgate 2>/dev/null || echo "warpgate network already exists"`)

	if networking != nil && len(networking.Traefik.EntryPoints) > 0 {
		traefikYAML, err := compose.GenerateTraefikCompose(networking)
		if err == nil {
			// Escape single quotes for shell heredoc
			escaped := strings.ReplaceAll(traefikYAML, "'", "'\\''")
			parts = append(parts, fmt.Sprintf(`
echo "Setting up Traefik..."
cat > /opt/warpgate/traefik/compose.yml << 'TRAEFIKEOF'
%s
TRAEFIKEOF
cd /opt/warpgate/traefik && docker compose up -d
echo "Traefik started"`, escaped))
		}
	}

	return strings.Join(parts, "\n")
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
