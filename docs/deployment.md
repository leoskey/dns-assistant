# 部署检查与故障处理

更新：2026-10-27。支持 Docker 的服务器、电脑和 NAS 均可部署。执行 [README](../README.md) 中的 Compose 步骤。以下检查不修改路由器、防火墙或现有 DNS。

## 安装前

1. 用 `docker version` 与 `docker compose version` 确认 daemon 和 Compose v2。
2. 部署设备通过 DHCP 保留或静态配置拥有稳定的局域网地址；在 .env 填 SERVER_IP。旧 NAS_IP 仍可使用，同时存在时 SERVER_IP 优先。
3. 检查 TCP／UDP 53、TCP 8080 和 TCP 8082 的现有占用，运行 `sh deploy/preflight.sh` 辅助诊断。
4. Docker named volumes 位于本地存储，不使用 NFS／SMB 作为 SQLite WAL 数据目录。
5. 镜像运行 UID/GID 为 10001:10001。默认新命名卷继承镜像 /data 的所有权；若替换成 bind mount，预先给目录匹配的读写权限，勿把无关目录授权给容器。
6. 运行 keygen 生成随机服务密钥，两个容器使用同一值；保留 .env，不提交到 Git。
7. `docker compose config --quiet` 通过后启动，`docker compose ps` 应显示两个 healthy。
8. Compose 对外仅发布 IPv4 端口，SERVER_IP 必须填写局域网 IPv4。目标下载域名 AAAA 返回 NODATA，普通域名 AAAA 仍可经 IPv4 DNS 查询。DNS 核心库可能在容器内创建双栈通配套接字，不能依据日志中的 `[::]` 判断宿主机发布了 IPv6；不要额外增加 IPv6 端口发布或来源网段。

默认从 `ghcr.io/leoskey/dns-assistant:latest` 拉取统一双架构镜像，Docker 自动匹配 amd64／arm64；无需在部署设备编译源码。可用 `DNS_ASSISTANT_IMAGE` 固定版本或摘要。源码构建使用 `-f compose.yaml -f compose.build.yaml`，之后的启动／重启等命令保持同一组覆盖文件。未发布的源码变更需源码构建后使用，不假定 latest 已包含代理功能。

中国大陆访问 GHCR 缓慢时，可将 DNS_ASSISTANT_IMAGE 设为 `ghcr.nju.edu.cn/leoskey/dns-assistant:latest`，再直接 pull 与 up。现有 fnOS NAS 已验证该 GHCR 缓存路径，具体操作、摘要核对与回退方式见 [NAS 拉取加速与升级](image-release.md#nas-拉取加速与升级)。

更新时保留原项目名称、服务密钥和两个卷，执行 `docker compose pull`、`docker compose up -d --wait`。已有 fnOS 部署使用 `settings.env` 时保留其 `env_file`，仅替换两个服务的 image，并移除旧的 `pull_policy: never`。发布与匿名拉取检查见 [镜像发布维护](image-release.md)。

## 接入

浏览器打开 http://SERVER_IP:8080，无需登录。管理与代理仅用于受信任局域网，勿转发这些端口至公网。主机名访问使用 ADMIN_HOSTS 允许列表；反向代理应限制来源，不把所有转发来源当作外网访问的保护措施。

原生运行未声明地址时，页面按本机网卡顺序列出局域网 IPv4，默认第一项；有多项时可下拉选择，DNS 和代理两个参数区同步。回环、公网、链路本地和 IPv6 不展示。容器使用显式声明的 SERVER_IP（兼容 NAS_IP），不自动把桥接网卡地址当作宿主机地址；多网卡仅可使用实际发布服务的地址。原生枚举可能包含虚拟网卡的私网地址，系统顺序不代表其他设备一定能连接；选择与主机连通的网段。

DNS 接入：游戏主机只填写 DNS 地址 SERVER_IP，不手填端口；服务端仍须发布标准 TCP／UDP 53（见 [IANA 端口登记](https://www.iana.org/assignments/service-names-port-numbers?search=domain)）。页面隐藏 DNS 端口行，代理仍填写 8082。重新运行网络测试，观察“解析历史”中的真实来源与域名。Windows Docker Desktop 的转发可能合并来源，不能据此声称已识别每台主机。

代理模式：随 controller 启动，无需开关；首页同时显示代理与 DNS 参数，在“配置方式”卡片选择对应指引。将支持手动代理的主机服务器地址设为 SERVER_IP，端口 8082，DNS 可保持原设置。只改 DNS 不会接入代理；没有代理设置入口的主机继续使用 DNS。停止 controller 前先在主机选择“不使用代理”。完整步骤和限制见 [下载代理](download-proxy.md)。

有下载节点拒绝测试内容时，查看平台页与历史错误，不强制绑定该 IP。Xbox com／cn 不互换 IP；Nintendo 的联网测试地址与游戏下载不同，实际下载域名必须提供对应有效样本。

查询与测试记录自动按时间及数量清理，无需手动清空。连接记录和 DNS 查询各最多保留 7 天／10,000 条，测速结果 30 天／5,000 条，运行事件 30 天／500 条；启动及每小时检查时间范围。SQLite 会复用已释放页面，文件不必立即缩小。两个容器的运行日志各自轮换保留最多 3 个 10 MiB 文件。Docker 的本地存活检查不会下载 CDN 文件或更新优选 IP。记录接口的分页与保留信息见 [管理 API](api.md)。

本版 controller 将数据库 schema 1/2/3 自动升级为 schema 4（新增连接记录表），原配置、合法 IPv4 策略与历史保留，移除旧预算表及 IPv6 候选。旧配置中的 IPv6 上游会移除，没有 IPv4 上游时使用内置 IPv4 上游；切换优势与冷却迁移到当前默认 5%／5 分钟。首次升级前保留 controller 数据卷快照；若回退到仅支持 schema 1/2/3 的旧镜像，需要同时恢复升级前的数据卷。配置 JSON 导出不包含历史、汇总及调度时间，不能代替完整数据卷快照。

## 常见情况

| 现象 | 处理 |
| --- | --- |
| 53 端口占用 | 确认现有服务；不强制结束别的程序。可以为容器规划独立 IP，按 NAS 型号验证 Macvlan 支持及宿主机通信限制 |
| 管理入口 403 | 检查访问来源网段、URL 中的 Host 与 ADMIN_HOSTS；默认直接使用部署设备的局域网 IP |
| DNS 核心尚未连接 | 检查两个容器 healthy、SERVICE_TOKEN 一致、内部网络与日志；策略定期重传 |
| 测试样本过短 | 使用同域名且足够覆盖窗口的下载文件；不使用小文件或错误页面计算速度 |
| 规则表同步失败 | 查看设置中的错误；确认最终公网 HTTPS 地址、证书及 JSON 格式，原可用规则继续保留 |
| 403／404／内容不足 | 目标 CDN 或样本不可用；保留普通解析，替换真实同域名内容样本再测 |
| 没有主机查询 | 确认主机 DNS、访客网络隔离、宿主机防火墙和端口发布；代理方式不要求产生本服务 DNS 查询 |
| 代理无法连接／503 | 确认 TCP 8082 已发布、controller 正在运行、来源网段允许；活动请求达到 64 时等待或减少并发 |
| 代理 400／502 | 检查目标是否为公网 IPv4、HTTP 80／CONNECT 443，及配置的上游 DNS 与源站连通性；私网目标被拒绝 |
| 未使用预期节点 | 检查客户端是否使用其他 DNS、HTTPS／SVCB 地址提示或仍保持旧连接；DNS 不会重建现有下载连接 |
| 数据卷不可写 | 检查 UID/GID 和本地文件系统；修复后重启，避免使用 privileged 绕过 |
| 停止或升级服务 | 先把主机代理恢复为不使用，DNS 恢复自动或切换可用服务；升级前导出配置并保留两个数据卷 |

## 实机记录

按 [验收矩阵](acceptance.md) 填写部署设备系统／架构（NAS 部署可补充品牌型号）、家宽运营商、路由模式、主机型号、IPv4 接入、测试内容、时段、默认 DNS／优选 DNS 或直连／代理多轮完成时间与失败次数。Docker Desktop 通过不能代替目标设备和真实主机记录。
