#!/bin/sh
set -eu

image=${1:?Usage: sh scripts/test-container.sh IMAGE}
name="dns-assistant-test-$$"
cleanup() {
    docker logs "$name" || true
    docker rm -f "$name" || true
    docker volume rm "$name-data" || true
}
trap cleanup EXIT

start() {
    docker run -d --name "$name" --network none --read-only --cap-drop ALL \
        --security-opt no-new-privileges:true \
        -v "$name-data:/data" "$image"
}
start
for attempt in 1 2; do
    ready=false
    for retry in $(seq 1 20); do
        if docker exec "$name" dns-assistant health && docker exec "$name" test -s /data/dns/policy.json; then
            ready=true
            break
        fi
        sleep 1
    done
    "$ready"
    docker exec "$name" dns-assistant health dns
    docker exec "$name" sh -c 'test "$(id -u)" = 10001 && test -s /data/controller/assistant.db && test -s /data/dns/policy.json'
    if [ "$attempt" = 1 ]; then
        docker exec "$name" sh -c 'echo retained > /data/controller/persistence-test; echo retained > /data/dns/persistence-test'
        docker stop --time 20 "$name"
        test "$(docker inspect --format '{{.State.ExitCode}}' "$name")" = 0
        docker rm "$name"
        start
    else
        docker exec "$name" sh -c 'test "$(cat /data/controller/persistence-test)" = retained && test "$(cat /data/dns/persistence-test)" = retained'
        docker stop --time 20 "$name"
        test "$(docker inspect --format '{{.State.ExitCode}}' "$name")" = 0
    fi
done
