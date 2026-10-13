#!/bin/sh
set -eu

cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
command -v docker >/dev/null 2>&1 || { echo "Docker is required." >&2; exit 1; }

# Reuse the existing Relay's Compose project, so its service is updated in place.
# An unrelated service using port 8080 must never be stopped or replaced.
relay_ids=$(docker ps --filter publish=8080 --format '{{.ID}}')
project=dcode-relay
if [ -n "$relay_ids" ]; then
    count=$(printf '%s\n' "$relay_ids" | wc -l | tr -d ' ')
    [ "$count" = 1 ] || { echo "Multiple containers publish port 8080; inspect them before updating." >&2; exit 1; }
    service=$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.service" }}' "$relay_ids")
    project=$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$relay_ids")
    [ "$service" = relay ] && [ -n "$project" ] && [ "$project" != '<no value>' ] || {
        echo "Port 8080 belongs to a service that is not a Compose Relay; no changes were made." >&2
        exit 1
    }
fi

if docker compose version >/dev/null 2>&1; then
    docker compose -p "$project" -f docker-compose.yml up -d --build --no-deps relay
else
    docker-compose -p "$project" -f docker-compose.yml up -d --build --no-deps relay
fi

echo "Relay 0.1.11 deployed. Restart codex-remote run on Windows to display a six-digit connection code."
