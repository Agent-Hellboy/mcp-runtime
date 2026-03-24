#!/bin/bash
# dev-setup.sh - Setup development environment for mcp-runtime
# This script installs dev dependencies (controller-gen, kustomize, minikube) and generates code/manifests.
# End users don't need this - they use pre-generated manifests.
#
# Usage:
#   ./hack/dev-setup.sh [command]
#
# Commands:
#   install    - Install dev tools (controller-gen, kustomize)
#   generate   - Generate CRD manifests and DeepCopy methods
#   format     - Format code with go fmt
#   validate   - Validate code with go vet
#   minikube   - Install and start minikube cluster
#   all        - Run all steps (default)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

# Minikube configuration
MINIKUBE_CPUS="${MINIKUBE_CPUS:-2}"
MINIKUBE_MEMORY="${MINIKUBE_MEMORY:-4096}"
MINIKUBE_DISK_SIZE="${MINIKUBE_DISK_SIZE:-20g}"
MINIKUBE_DRIVER="${MINIKUBE_DRIVER:-docker}"
MINIKUBE_KUBERNETES_VERSION="${MINIKUBE_KUBERNETES_VERSION:-stable}"
MINIKUBE_PROFILE="${MINIKUBE_PROFILE:-mcp-runtime}"

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)
            if grep -q Microsoft /proc/version 2>/dev/null; then
                echo "wsl"
            else
                echo "linux"
            fi
            ;;
        Darwin*)
            echo "macos"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            echo "windows"
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)
            echo "amd64"
            ;;
        arm64|aarch64)
            echo "arm64"
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

# Check if Go is installed
check_go() {
    if ! command -v go &> /dev/null; then
        echo "❌ Error: Go is not installed. Please install Go to continue."
        echo "   Visit: https://golang.org/dl/"
        exit 1
    fi
}

# Check if Docker is installed and running
check_docker() {
    if ! command -v docker &> /dev/null; then
        echo "❌ Error: Docker is not installed. Please install Docker to continue."
        echo "   Visit: https://docs.docker.com/get-docker/"
        return 1
    fi

    if ! docker info &> /dev/null; then
        echo "❌ Error: Docker daemon is not running. Please start Docker."
        return 1
    fi

    return 0
}

# Check if kubectl is installed
check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        echo "⚠️  Warning: kubectl is not installed. Installing kubectl..."
        install_kubectl
    fi
}

# Install kubectl
install_kubectl() {
    local os=$(detect_os)
    local arch=$(detect_arch)

    echo "📦 Installing kubectl..."

    case "$os" in
        linux|wsl)
            curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${arch}/kubectl"
            chmod +x kubectl
            sudo mv kubectl /usr/local/bin/
            ;;
        macos)
            if command -v brew &> /dev/null; then
                brew install kubectl
            else
                curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/${arch}/kubectl"
                chmod +x kubectl
                sudo mv kubectl /usr/local/bin/
            fi
            ;;
        windows)
            echo "   Please install kubectl manually:"
            echo "   choco install kubernetes-cli"
            echo "   or download from: https://kubernetes.io/docs/tasks/tools/install-kubectl-windows/"
            return 1
            ;;
        *)
            echo "❌ Unsupported OS for automatic kubectl installation"
            return 1
            ;;
    esac

    echo "   ✓ kubectl installed successfully"
}

# Install minikube
install_minikube() {
    local os=$(detect_os)
    local arch=$(detect_arch)

    if command -v minikube &> /dev/null; then
        echo "   ✓ minikube already installed: $(minikube version --short)"
        return 0
    fi

    echo "📦 Installing minikube..."

    case "$os" in
        linux|wsl)
            echo "   Downloading minikube for Linux/${arch}..."
            curl -LO "https://storage.googleapis.com/minikube/releases/latest/minikube-linux-${arch}"
            chmod +x "minikube-linux-${arch}"
            sudo install "minikube-linux-${arch}" /usr/local/bin/minikube
            rm "minikube-linux-${arch}"
            ;;
        macos)
            if command -v brew &> /dev/null; then
                echo "   Installing via Homebrew..."
                brew install minikube
            else
                echo "   Downloading minikube for macOS/${arch}..."
                curl -LO "https://storage.googleapis.com/minikube/releases/latest/minikube-darwin-${arch}"
                chmod +x "minikube-darwin-${arch}"
                sudo install "minikube-darwin-${arch}" /usr/local/bin/minikube
                rm "minikube-darwin-${arch}"
            fi
            ;;
        windows)
            if command -v choco &> /dev/null; then
                echo "   Installing via Chocolatey..."
                choco install minikube -y
            elif command -v winget &> /dev/null; then
                echo "   Installing via winget..."
                winget install Kubernetes.minikube
            else
                echo "   Please install minikube manually:"
                echo "   - Using Chocolatey: choco install minikube"
                echo "   - Using winget: winget install Kubernetes.minikube"
                echo "   - Or download from: https://minikube.sigs.k8s.io/docs/start/"
                return 1
            fi
            ;;
        *)
            echo "❌ Unsupported OS: $os"
            echo "   Please install minikube manually from: https://minikube.sigs.k8s.io/docs/start/"
            return 1
            ;;
    esac

    echo "   ✓ minikube installed successfully: $(minikube version --short)"
}

# Start minikube cluster
start_minikube() {
    echo "🚀 Starting minikube cluster..."
    echo "   Profile: ${MINIKUBE_PROFILE}"
    echo "   Driver: ${MINIKUBE_DRIVER}"
    echo "   CPUs: ${MINIKUBE_CPUS}"
    echo "   Memory: ${MINIKUBE_MEMORY}MB"
    echo "   Disk: ${MINIKUBE_DISK_SIZE}"
    echo "   Kubernetes: ${MINIKUBE_KUBERNETES_VERSION}"
    echo ""

    # Check if cluster already exists and is running
    if minikube status -p "${MINIKUBE_PROFILE}" &> /dev/null; then
        local status=$(minikube status -p "${MINIKUBE_PROFILE}" -o json 2>/dev/null | grep -o '"Host":"[^"]*"' | cut -d'"' -f4)
        if [ "$status" = "Running" ]; then
            echo "   ✓ minikube cluster '${MINIKUBE_PROFILE}' is already running"
            return 0
        else
            echo "   Cluster exists but not running. Starting..."
            minikube start -p "${MINIKUBE_PROFILE}"
            echo "   ✓ minikube cluster started"
            return 0
        fi
    fi

    # Start new cluster
    minikube start \
        -p "${MINIKUBE_PROFILE}" \
        --driver="${MINIKUBE_DRIVER}" \
        --cpus="${MINIKUBE_CPUS}" \
        --memory="${MINIKUBE_MEMORY}" \
        --disk-size="${MINIKUBE_DISK_SIZE}" \
        --kubernetes-version="${MINIKUBE_KUBERNETES_VERSION}" \
        --addons=ingress \
        --addons=metrics-server \
        --addons=dashboard

    echo ""
    echo "   ✓ minikube cluster '${MINIKUBE_PROFILE}' started successfully"
}

# Stop minikube cluster
stop_minikube() {
    echo "🛑 Stopping minikube cluster..."

    if ! minikube status -p "${MINIKUBE_PROFILE}" &> /dev/null; then
        echo "   ⚠️  Cluster '${MINIKUBE_PROFILE}' does not exist"
        return 0
    fi

    minikube stop -p "${MINIKUBE_PROFILE}"
    echo "   ✓ minikube cluster stopped"
}

# Delete minikube cluster
delete_minikube() {
    echo "🗑️  Deleting minikube cluster..."

    if ! minikube status -p "${MINIKUBE_PROFILE}" &> /dev/null; then
        echo "   ⚠️  Cluster '${MINIKUBE_PROFILE}' does not exist"
        return 0
    fi

    read -p "   Are you sure you want to delete cluster '${MINIKUBE_PROFILE}'? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        minikube delete -p "${MINIKUBE_PROFILE}"
        echo "   ✓ minikube cluster deleted"
    else
        echo "   Cancelled"
    fi
}

# Show minikube status
status_minikube() {
    echo "📊 Minikube cluster status..."
    echo ""

    if ! command -v minikube &> /dev/null; then
        echo "   ❌ minikube is not installed"
        return 1
    fi

    if ! minikube status -p "${MINIKUBE_PROFILE}" &> /dev/null; then
        echo "   ⚠️  Cluster '${MINIKUBE_PROFILE}' does not exist"
        echo "   Run './hack/dev-setup.sh minikube start' to create it"
        return 0
    fi

    minikube status -p "${MINIKUBE_PROFILE}"
    echo ""
    echo "   Kubernetes context: $(kubectl config current-context 2>/dev/null || echo 'not set')"
    echo ""

    # Show node info
    echo "   Node info:"
    kubectl get nodes -o wide 2>/dev/null || echo "   Unable to get node info"
}

# Configure kubectl to use minikube
configure_kubectl() {
    echo "🔧 Configuring kubectl to use minikube..."

    if ! minikube status -p "${MINIKUBE_PROFILE}" &> /dev/null; then
        echo "   ❌ Cluster '${MINIKUBE_PROFILE}' is not running"
        return 1
    fi

    kubectl config use-context "${MINIKUBE_PROFILE}"
    echo "   ✓ kubectl configured to use context '${MINIKUBE_PROFILE}'"
}

# Setup minikube (install + start)
setup_minikube() {
    echo "🔧 Setting up minikube development cluster..."
    echo ""

    # Check Docker first (default driver)
    if [ "${MINIKUBE_DRIVER}" = "docker" ]; then
        if ! check_docker; then
            echo ""
            echo "💡 Tip: You can use a different driver by setting MINIKUBE_DRIVER"
            echo "   Example: MINIKUBE_DRIVER=hyperkit ./hack/dev-setup.sh minikube"
            exit 1
        fi
        echo "✓ Docker is installed and running"
        echo ""
    fi

    # Check/install kubectl
    check_kubectl
    echo ""

    # Install minikube
    install_minikube
    echo ""

    # Start minikube
    start_minikube
    echo ""

    # Configure kubectl
    configure_kubectl
    echo ""

    echo "✅ Minikube setup complete!"
    echo ""
    echo "Useful commands:"
    echo "  minikube dashboard -p ${MINIKUBE_PROFILE}    # Open Kubernetes dashboard"
    echo "  minikube tunnel -p ${MINIKUBE_PROFILE}       # Enable LoadBalancer services"
    echo "  minikube ssh -p ${MINIKUBE_PROFILE}          # SSH into the minikube node"
    echo "  ./hack/dev-setup.sh minikube status          # Check cluster status"
    echo "  ./hack/dev-setup.sh minikube stop            # Stop the cluster"
    echo "  ./hack/dev-setup.sh minikube delete          # Delete the cluster"
}

# Install dev tools
install_tools() {
    echo "📦 Installing dev tools..."
    echo ""
    
    # Install controller-gen
    echo "📦 Installing controller-gen..."
    if [ ! -f "bin/controller-gen" ]; then
        echo "   Downloading controller-gen..."
        make -f Makefile.operator controller-gen
        echo "   ✓ controller-gen installed to bin/controller-gen"
    else
        echo "   ✓ controller-gen already installed"
    fi
    echo ""
    
    # Install kustomize
    echo "📦 Installing kustomize..."
    if [ ! -f "bin/kustomize" ]; then
        echo "   Downloading kustomize..."
        make -f Makefile.operator kustomize
        echo "   ✓ kustomize installed to bin/kustomize"
    else
        echo "   ✓ kustomize already installed"
    fi
    echo ""
    
    echo "✅ Tools installation complete!"
}

# Generate manifests and code
generate_code() {
    echo "📝 Generating code and manifests..."
    echo ""
    
    # Generate CRD manifests
    echo "📝 Generating CRD manifests..."
    make -f Makefile.operator manifests
    echo "   ✓ CRD manifests generated in config/crd/bases/"
    echo ""
    
    # Generate DeepCopy methods
    echo "📝 Generating DeepCopy methods..."
    make -f Makefile.operator generate
    echo "   ✓ DeepCopy methods generated in api/v1alpha1/zz_generated.deepcopy.go"
    echo ""
    
    echo "✅ Code generation complete!"
}

# Format code
format_code() {
    echo "🎨 Formatting code..."
    make -f Makefile.operator fmt
    echo "   ✓ Code formatted"
    echo ""
    echo "✅ Formatting complete!"
}

# Validate code
validate_code() {
    echo "🔍 Validating code..."
    make -f Makefile.operator vet
    echo "   ✓ Code validated"
    echo ""
    echo "✅ Validation complete!"
}

# Show usage
show_usage() {
    cat << EOF
🔧 MCP Runtime Development Setup
==================================

Usage: ./hack/dev-setup.sh [command]

Commands:
  install              Install dev tools (controller-gen, kustomize)
  generate             Generate CRD manifests and DeepCopy methods
  format               Format code with go fmt
  validate             Validate code with go vet
  minikube [action]    Manage minikube cluster (default: setup)
  all                  Run all steps (default)

Minikube Actions:
  minikube             Install and start minikube (alias for 'minikube setup')
  minikube setup       Install minikube and start cluster
  minikube start       Start existing cluster (or create new)
  minikube stop        Stop the cluster
  minikube delete      Delete the cluster
  minikube status      Show cluster status

Examples:
  ./hack/dev-setup.sh install           # Install tools only
  ./hack/dev-setup.sh generate          # Generate manifests only
  ./hack/dev-setup.sh format            # Format code only
  ./hack/dev-setup.sh validate          # Validate code only
  ./hack/dev-setup.sh minikube          # Setup minikube cluster
  ./hack/dev-setup.sh minikube status   # Check minikube status
  ./hack/dev-setup.sh minikube stop     # Stop minikube
  ./hack/dev-setup.sh all               # Run everything
  ./hack/dev-setup.sh                   # Run everything (default)

Environment Variables (minikube):
  MINIKUBE_CPUS                CPU count (default: 2)
  MINIKUBE_MEMORY              Memory in MB (default: 4096)
  MINIKUBE_DISK_SIZE           Disk size (default: 20g)
  MINIKUBE_DRIVER              Driver (default: docker)
  MINIKUBE_KUBERNETES_VERSION  K8s version (default: stable)
  MINIKUBE_PROFILE             Profile name (default: mcp-runtime)

Note: End users don't need these tools - they use pre-generated manifests.
EOF
}

# Main execution
COMMAND="${1:-all}"
SUBCOMMAND="${2:-}"

case "$COMMAND" in
    install)
        check_go
        echo "✓ Go is installed: $(go version)"
        echo ""
        install_tools
        ;;
    generate)
        check_go
        echo "✓ Go is installed: $(go version)"
        echo ""
        generate_code
        ;;
    format)
        check_go
        format_code
        ;;
    validate)
        check_go
        validate_code
        ;;
    minikube)
        case "$SUBCOMMAND" in
            ""|setup)
                setup_minikube
                ;;
            start)
                start_minikube
                ;;
            stop)
                stop_minikube
                ;;
            delete)
                delete_minikube
                ;;
            status)
                status_minikube
                ;;
            config)
                configure_kubectl
                ;;
            *)
                echo "❌ Unknown minikube action: $SUBCOMMAND"
                echo ""
                echo "Available actions: setup, start, stop, delete, status, config"
                exit 1
                ;;
        esac
        ;;
    all)
        check_go
        echo "🔧 MCP Runtime Development Setup"
        echo "=================================="
        echo ""
        echo "✓ Go is installed: $(go version)"
        echo ""

        install_tools
        echo ""
        generate_code
        echo ""
        format_code
        echo ""
        validate_code
        echo ""

        # Ask about minikube setup
        echo "────────────────────────────────────────"
        echo ""
        read -p "🔧 Would you like to setup minikube cluster? [y/N] " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo ""
            setup_minikube
        else
            echo "   Skipping minikube setup"
            echo "   Run './hack/dev-setup.sh minikube' later to set it up"
        fi
        echo ""

        echo "✅ Development setup complete!"
        echo ""
        echo "Next steps:"
        echo "  - Make changes to api/v1alpha1/mcpserver_types.go"
        echo "  - Run './hack/dev-setup.sh generate' to regenerate manifests"
        echo "  - Or use: make -f Makefile.operator manifests generate"
        echo ""
        echo "Tools installed:"
        echo "  - bin/controller-gen (for generating CRDs and RBAC)"
        echo "  - bin/kustomize (for building Kubernetes manifests)"
        if command -v minikube &> /dev/null; then
            echo "  - minikube (for local Kubernetes cluster)"
        fi
        ;;
    help|--help|-h)
        show_usage
        ;;
    *)
        echo "❌ Unknown command: $COMMAND"
        echo ""
        show_usage
        exit 1
        ;;
esac

