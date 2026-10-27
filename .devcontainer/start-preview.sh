#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
preview_token_file=/tmp/dns-assistant-preview-token
if [ ! -s "$preview_token_file" ]; then
  (umask 077; openssl rand -hex 32 > "$preview_token_file")
fi
export SERVICE_TOKEN="$(cat "$preview_token_file")"
export ADMIN_HOSTS="localhost,127.0.0.1,${CODESPACE_NAME:-preview}-8088.${GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN:-app.github.dev}"
docker compose -f .devcontainer/compose.preview.yaml up --build -d
for attempt in $(seq 1 30); do
  if curl --fail --silent http://localhost:8088/healthz > /dev/null; then
    echo "DNS Assistant ready on forwarded port 8088"
    exit 0
  fi
  sleep 2
done
docker compose -f .devcontainer/compose.preview.yaml logs --tail 40
exit 1
