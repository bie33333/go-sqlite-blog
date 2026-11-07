#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if ! command -v docker >/dev/null 2>&1; then
    echo "Error: Docker is required to redeploy this application." >&2
    exit 1
fi

if ! docker info >/dev/null 2>&1; then
    echo "Error: cannot access the Docker daemon. Run with sudo or grant this user Docker access." >&2
    exit 1
fi

if [[ ! -f .env ]]; then
    echo "Error: .env is missing. Create it from .env.example and set DOMAIN first." >&2
    exit 1
fi

docker compose version >/dev/null
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
