#!/bin/sh
set -eu
echo "DNS Assistant 安装前检查"
docker version >/dev/null
docker compose version
echo "检查部署设备的固定局域网 IPv4；compose 的 SERVER_IP 必须属于本机。"
if command -v ip >/dev/null 2>&1; then ip -brief address; fi
echo "检查 TCP/UDP 53、管理端口 8080 与代理端口 8082 的占用："
if command -v ss >/dev/null 2>&1; then ss -lntup | awk 'NR==1 || /:53[[:space:]]|:8080[[:space:]]|:8082[[:space:]]/'; else echo "没有 ss，请使用系统的端口管理工具检查。"; fi
echo "Docker 数据根目录及文件系统："
docker info --format '{{.DockerRootDir}}'
echo "数据卷须位于本地文件系统；不要将 SQLite 数据目录放在 NFS/SMB 挂载。"
echo "配置 .env 后运行 docker compose config 校验。"
