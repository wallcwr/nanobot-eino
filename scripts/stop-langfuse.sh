#!/usr/bin/env bash
# Stop Langfuse and all dependencies.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.langfuse.yml"

if docker compose version &>/dev/null 2>&1; then
    docker compose -f "$COMPOSE_FILE" down
elif command -v docker-compose &>/dev/null; then
    docker-compose -f "$COMPOSE_FILE" down
else
    echo "Docker Compose not found."
    exit 1
fi

echo "Langfuse stopped."
