#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"

for argument do
    case "$argument" in
        --env-file|--env-file=*)
            echo "自定义环境文件请显式设置 SERVER_IP，并使用原有 docker compose --env-file 命令。" >&2
            exit 1
            ;;
    esac
done

# Let Compose parse .env without evaluating it as shell code or requiring service variables.
environment=$(printf 'services: {}\n' | docker compose -f - config --environment)
SERVER_IP=$(printf '%s\n' "$environment" | sed -n 's/^SERVER_IP=//p')
unset environment

if [ -z "$SERVER_IP" ]; then
    if ! command -v ip >/dev/null 2>&1; then
        echo "无法自动检测地址：需要 Linux ip 命令；请在 .env 中设置 SERVER_IP。" >&2
        exit 1
    fi
    endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
    if [ -z "${DOCKER_CONTEXT:-}" ] && [ -n "${DOCKER_HOST:-}" ]; then
        endpoint=$DOCKER_HOST
    fi
    case "$endpoint" in
        unix://*) ;;
        *) echo "当前 Docker 环境需要在 .env 中显式设置 SERVER_IP。" >&2; exit 1 ;;
    esac
    # Route lookup selects a local source address without sending traffic to this destination.
    route=$(ip -4 route get 192.0.2.1 2>/dev/null) || route=
    SERVER_IP=$(printf '%s\n' "$route" | awk '{for(i=1;i<NF;i++) if($i=="src") {print $(i+1);exit}}')
    interface=$(printf '%s\n' "$route" | awk '{for(i=1;i<NF;i++) if($i=="dev") {print $(i+1);exit}}')
    if [ -z "$SERVER_IP" ] || [ -z "$interface" ] || ! ip -o link show up dev "$interface" | grep -q '<[^>]*BROADCAST'; then
        echo "无法确认局域网出口地址；请在 .env 中设置 SERVER_IP。" >&2
        exit 1
    fi
    if ! ip -o -4 address show dev "$interface" scope global | awk -v ip="$SERVER_IP" '$3=="inet" {split($4,a,"/");if(a[1]==ip) found=1} END {exit !found}'; then
        echo "检测到的地址不属于出口网卡；请在 .env 中设置 SERVER_IP。" >&2
        exit 1
    fi
fi

if ! printf '%s\n' "$SERVER_IP" | awk -F. '
    NF!=4 {exit 1}
    {for(i=1;i<=4;i++) if($i !~ /^(0|[1-9][0-9]*)$/ || $i>255) exit 1
     if(!($1==10 || ($1==172 && $2>=16 && $2<=31) || ($1==192 && $2==168))) exit 1}'; then
    echo "SERVER_IP 必须是局域网 IPv4；请检查 .env 或显式指定地址。" >&2
    exit 1
fi

printf '服务地址：%s\n' "$SERVER_IP" >&2
export SERVER_IP
exec docker compose "$@"
