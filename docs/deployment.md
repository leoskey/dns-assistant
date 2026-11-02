# Docker 部署与升级

DNS、下载代理和管理功能在一个进程、一个容器中运行。直接使用 [README 的 Compose 配置](../README.md#docker-使用)，无需服务密钥或 `.env` 文件。服务停止时，DNS 与代理连接一并停止。

## 部署

1. 确认 Docker Engine 和 Compose v2 可用，服务器具有稳定的局域网 IPv4。
2. 确认 TCP／UDP 53、TCP 8080 和 8082 未被占用；`sh deploy/preflight.sh` 可辅助检查。
3. 执行 `docker compose config`、`docker compose pull`、`docker compose up -d --wait`。
4. 打开 `http://服务器局域网IP:8080`。游戏主机的 DNS 或代理地址使用服务器的局域网 IP，代理端口为 8082。

容器只发布 IPv4 入口，来源仍受应用的允许网段限制。管理界面免登录，仅用于受信任局域网；不要将管理端口转发至公网。需要自定义管理域名时，在 `app.environment` 中设置 `ADMIN_HOSTS`。容器无法从桥接网卡可靠识别宿主机 IP，管理页可能没有可复制的接入地址，此时按服务器实际地址填写。

默认使用 `ghcr.io/leoskey/dns-assistant:latest`。固定版本或镜像摘要时直接修改 `services.app.image`。源码构建使用 `docker compose -f compose.yaml -f compose.build.yaml up -d --build --wait`；之后的操作继续带同样的两个 `-f` 参数。

`deploy/compose.sh` 仅在仓库目录转发 `docker compose` 的参数，不再探测地址或生成配置。

## 数据与权限

全部持久数据保存在一个 `data` 命名卷中，挂载到 `/data`：

| 卷内路径 | 内容 |
| --- | --- |
| controller/assistant.db | SQLite 数据库、配置、记录和调度 |
| dns/policy.json | DNS 策略快照 |

镜像以 UID/GID `10001:10001` 运行，新卷继承目录所有权。改用宿主机目录挂载时，需要对应的读写权限；数据库目录使用本地文件系统，不使用 NFS／SMB。配置导出不包含历史与调度，不能替代完整数据卷备份。

查询最多保留 7 天／10,000 条，测速结果 30 天／5,000 条，运行事件 30 天／500 条；启动及每小时清理。数据库复用已释放页面。容器日志轮换保留最多 3 个 10 MiB 文件。健康检查同时访问数据库和本地 DNS，不下载 CDN 内容。

## 更新与备份

日常更新使用 `docker compose pull` 与 `docker compose up -d --wait`，保持项目名称及 `data` 卷不变。备份时先停止程序，再保存整个数据卷；回退时使用匹配的镜像和数据卷备份。不要使用 `down -v`，该命令会删除数据卷。

## 故障检查

| 现象 | 处理 |
| --- | --- |
| 端口占用 | 检查现有 DNS／代理程序或旧容器，确认归属后调整部署 |
| 管理入口 403 | 检查访问来源网段、Host 与 ADMIN_HOSTS，优先使用服务器局域网 IP |
| DNS 未就绪 | 查看 `docker compose logs app`，检查监听端口、策略快照及上游配置 |
| 数据卷不可写 | 检查 UID/GID 与挂载路径，避免使用 privileged 绕过权限问题 |
| 没有主机查询 | 检查主机 DNS、访客网络隔离、防火墙和端口发布 |
| 未使用预期节点 | 检查主机缓存、其他 DNS 及已有连接；策略更新不会迁移旧连接 |

真实主机接入与下载效果按 [验收矩阵](acceptance.md) 检查。Docker Desktop 可能合并请求来源，其结果不能替代目标服务器与真实游戏主机验证。
