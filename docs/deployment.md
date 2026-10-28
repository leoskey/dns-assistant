# Docker 部署检查与故障处理

更新：2026-10-28。本文提供通用部署与维护步骤；验证范围见 [验证记录](validation.md)。以下检查不修改路由器、防火墙或现有 DNS。

当前说明覆盖通用 Docker 环境，不限定硬件品牌或设备类别；`SERVER_IP` 表示服务主机的局域网 IPv4。

## 安装前

无需 Docker 的 Windows x64 安装程序和便携版见 [Windows 单机安装](windows.md)。两种方式独立选择，不要在同一 IP 上同时占用 53 和 8080。

1. 用 `docker version` 与 `docker compose version` 确认 daemon 和 Compose v2。
2. 服务主机通过 DHCP 保留或静态配置拥有稳定的局域网地址；在 .env 填 SERVER_IP。
3. 检查 TCP／UDP 53 和 TCP 8080 的现有占用，运行 `sh deploy/preflight.sh` 辅助诊断。
4. Docker named volumes 位于服务主机本地存储，不使用 NFS／SMB 作为 SQLite WAL 数据目录。
5. 镜像运行 UID/GID 为 10001:10001。默认新命名卷继承镜像 /data 的所有权；若替换成 bind mount，预先给目录匹配的读写权限，勿把整个服务主机目录授权给容器。
6. 运行 keygen 生成随机服务密钥，两个容器使用同一值；保留 .env，不提交到 Git。
7. 按选定部署方式执行 Compose 配置校验并启动，两个服务均应显示 healthy。
8. Compose 对外仅发布 IPv4 端口，SERVER_IP 必须填写局域网 IPv4。目标下载域名 AAAA 返回 NODATA，普通域名 AAAA 仍可经 IPv4 DNS 查询。DNS 核心库可能在容器内创建双栈通配套接字，不能依据日志中的 `[::]` 判断宿主机发布了 IPv6；不要额外增加 IPv6 端口发布或来源网段。

单独使用 `compose.yaml` 时，默认从 `ghcr.io/leoskey/dns-assistant:latest` 拉取发布镜像，Docker 按 manifest 匹配 amd64／arm64；可用 `DNS_ASSISTANT_IMAGE` 固定已发布版本或摘要。部署前核对目标发布的版本、提交与架构，源码更新不代表镜像已更新。[README](../README.md) 的快速部署采用源码构建，使用 `-f compose.yaml -f compose.build.yaml`，之后的校验、启动与维护命令保持同一组覆盖文件。

中国大陆访问 GHCR 缓慢时，可按 [镜像拉取与升级](image-release.md#镜像拉取与升级) 检查缓存入口的可达性与摘要，再选择拉取路径。

更新时保留原项目名称、环境文件、服务密钥和两个数据卷。镜像部署执行 `docker compose pull`、`docker compose up -d --wait`；源码部署按 README 使用两个 `-f` 参数重新 build 与 up。自定义 Compose 使用 `env_file` 时保留其引用；由离线镜像转为网络拉取时，移除阻止拉取的 `pull_policy: never`。发布与匿名拉取检查见 [镜像发布维护](image-release.md)。

## 接入

浏览器打开 http://SERVER_IP:8080，无需登录。管理仅用于受信任局域网，勿转发管理端口至公网。主机名访问使用 ADMIN_HOSTS 允许列表；服务主机反向代理应限制来源，不把所有转发来源当作外网访问的保护措施。

游戏主机将 DNS 设置为 SERVER_IP，重新运行网络测试，观察“接入检查”中的真实来源与域名。Windows Docker Desktop 的转发可能合并来源，不能据此声称已识别每台主机。

有下载节点拒绝测试内容时，查看平台页与历史错误，不强制绑定该 IP。Xbox com／cn 不互换 IP；Nintendo 的联网测试地址与游戏下载不同，实际下载域名必须提供对应有效样本。

查询与测试记录自动按时间及数量清理，无需手动清空。查询最多保留 7 天／10,000 条，测速结果 30 天／5,000 条，运行事件 30 天／500 条；启动及每小时检查时间范围。SQLite 会复用已释放页面，文件不必立即缩小。两个容器的运行日志各自轮换保留最多 3 个 10 MiB 文件。Docker 的本地存活检查不会下载 CDN 文件或更新优选 IP。记录接口的分页与保留信息见 [管理 API](api.md)。

本版 controller 将数据库 schema 1/2 自动升级为 schema 3，原配置、合法 IPv4 策略与历史保留，移除旧预算表及 IPv6 候选。旧配置中的 IPv6 上游会移除，没有 IPv4 上游时使用内置 IPv4 上游；切换优势与冷却迁移到当前默认 5%／5 分钟。首次升级前保留 controller 数据卷快照；若回退到仅支持 schema 1/2 的旧镜像，需要同时恢复升级前的数据卷。配置 JSON 导出不包含历史、汇总及调度时间，不能代替完整数据卷快照。

## 常见情况

| 现象 | 处理 |
| --- | --- |
| 53 端口占用 | 确认现有服务；不强制结束别的程序。可以为容器规划独立 IP，按目标系统验证 Macvlan 支持及宿主机通信限制 |
| 管理入口 403 | 检查访问来源网段、URL 中的 Host 与 ADMIN_HOSTS；默认直接使用服务主机的局域网 IP |
| DNS 核心尚未连接 | 检查两个容器 healthy、SERVICE_TOKEN 一致、内部网络与日志；策略定期重传 |
| 测试样本过短 | 使用同域名且足够覆盖窗口的下载文件；不使用小文件或错误页面计算速度 |
| 规则表同步失败 | 查看设置中的错误；确认最终公网 HTTPS 地址、证书及 JSON 格式，原可用规则继续保留 |
| 403／404／内容不足 | 目标 CDN 或样本不可用；保留普通解析，替换真实同域名内容样本再测 |
| 没有主机查询 | 确认主机 DNS、访客网络隔离、服务主机防火墙和端口发布 |
| 未使用预期节点 | 检查客户端是否使用其他 DNS、HTTPS／SVCB 地址提示或仍保持旧连接；DNS 不会重建现有下载连接 |
| 数据卷不可写 | 检查 UID/GID 和本地文件系统；修复后重启，避免使用 privileged 绕过 |
| 停止或升级服务主机 | 先确认其他解析路径或把主机 DNS 改回自动；升级前导出配置并保留两个数据卷 |

## 实机验收

按 [验收矩阵](acceptance.md) 和 [空白模板](field-validation-template.md) 检查目标环境、IPv4 接入、重启恢复及真实下载对比。填写后的设备信息、地址、查询记录、日志与截图保存在仓库外；仓库仅保留脱敏的验证范围、结论和未验证项。Docker Desktop 通过不能代替目标设备验收。
