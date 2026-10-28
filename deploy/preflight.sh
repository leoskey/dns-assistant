#!/bin/sh
set -eu
echo "DNS Assistant 安装前检查"
docker version >/dev/null
docker compose version
echo "检查服务主机的稳定局域网 IPv4；sh deploy/compose.sh 可自动检测，也可显式设置 SERVER_IP。"
if command -v ip >/dev/null 2>&1; then ip -brief address; fi
echo "检查 TCP/UDP 53 与管理端口 8080 与代理端口 8082 的占用："
if command -v ss >/dev/null 2>&1; then ss -lntup | awk 'NR==1 || /:53[[:space:]]|:8080[[:space:]]|:8082[[:space:]]/'; else echo "没有 ss，请使用服务主机端口管理界面检查。"; fi
echo "Docker 数据根目录及文件系统："
docker info --format '{{.DockerRootDir}}'
echo "数据卷须位于服务主机本地文件系统；不要将 SQLite 数据目录放在 NFS/SMB 挂载。"
echo "配置 .env 的服务密钥后运行 sh deploy/compose.sh config --quiet 校验。"
