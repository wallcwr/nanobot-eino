#!/usr/bin/env bash
# Start Langfuse and all dependencies via Docker Compose.
# Works on Linux and macOS. Auto-installs Docker if not found.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.langfuse.yml"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# --- Docker installation ---
install_docker_linux() {
    info "Installing Docker via official install script..."
    curl -fsSL https://get.docker.com | sh
    sudo systemctl enable docker
    sudo systemctl start docker
    # Add current user to docker group so sudo is not needed next time
    sudo usermod -aG docker "$USER" 2>/dev/null || true
    info "Docker installed. You may need to log out and back in for group changes."
}

install_docker_macos() {
    if command -v brew &>/dev/null; then
        info "Installing Docker Desktop via Homebrew..."
        brew install --cask docker
    else
        error "Homebrew not found. Please install Docker Desktop manually: https://docs.docker.com/desktop/install/mac-install/"
    fi
    info "Starting Docker Desktop..."
    open -a Docker
    # Wait for Docker daemon to be ready
    local retries=30
    while ! docker info &>/dev/null && [ $retries -gt 0 ]; do
        sleep 2
        retries=$((retries - 1))
    done
    if ! docker info &>/dev/null; then
        error "Docker Desktop started but daemon is not ready. Please wait and re-run this script."
    fi
}

ensure_docker() {
    if command -v docker &>/dev/null && docker info &>/dev/null 2>&1; then
        info "Docker is running."
        return
    fi

    if command -v docker &>/dev/null; then
        warn "Docker is installed but the daemon is not running."
        case "$(uname -s)" in
            Darwin)
                info "Attempting to start Docker Desktop..."
                open -a Docker 2>/dev/null || true
                local retries=30
                while ! docker info &>/dev/null && [ $retries -gt 0 ]; do
                    sleep 2
                    retries=$((retries - 1))
                done
                docker info &>/dev/null || error "Could not start Docker daemon. Please start Docker Desktop manually."
                ;;
            Linux)
                info "Attempting to start Docker daemon..."
                sudo systemctl start docker 2>/dev/null || sudo service docker start 2>/dev/null || \
                    error "Could not start Docker daemon. Run: sudo systemctl start docker"
                ;;
        esac
        return
    fi

    warn "Docker is not installed."
    echo -n "Install Docker automatically? [y/N] "
    read -r answer
    case "$answer" in
        [yY]|[yY][eE][sS])
            case "$(uname -s)" in
                Linux)  install_docker_linux ;;
                Darwin) install_docker_macos ;;
                *)      error "Unsupported OS: $(uname -s). Please install Docker manually." ;;
            esac
            ;;
        *)
            error "Docker is required. Install it from https://docs.docker.com/get-docker/"
            ;;
    esac
}

# --- Docker Compose check ---
ensure_compose() {
    if docker compose version &>/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    elif command -v docker-compose &>/dev/null; then
        COMPOSE_CMD="docker-compose"
    else
        error "Docker Compose not found. Please install Docker Compose: https://docs.docker.com/compose/install/"
    fi
    info "Using: $COMPOSE_CMD"
}

# --- Main ---
main() {
    info "=== Langfuse Startup Script ==="
    ensure_docker
    ensure_compose

    info "Starting Langfuse services..."
    $COMPOSE_CMD -f "$COMPOSE_FILE" up -d

    info "Waiting for Langfuse to be ready..."
    local retries=60
    while ! curl -sf http://localhost:3000 &>/dev/null && [ $retries -gt 0 ]; do
        sleep 3
        retries=$((retries - 1))
    done

    if curl -sf http://localhost:3000 &>/dev/null; then
        echo ""
        info "=== Langfuse is ready! ==="
        info "UI:            http://localhost:3000"
        info "MinIO Console: http://localhost:9090"
        echo ""
        info "To stop:  $COMPOSE_CMD -f $COMPOSE_FILE down"
        info "To logs:  $COMPOSE_CMD -f $COMPOSE_FILE logs -f"
    else
        warn "Langfuse may still be starting. Check logs:"
        warn "  $COMPOSE_CMD -f $COMPOSE_FILE logs -f langfuse"
    fi
}

main "$@"
