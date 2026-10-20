# NAS 部署检查与故障处理

更新：2026-10-20。执行 [README](../README.md) 中的 Compose 步骤。以下检查不修改路由器、防火墙或现有 DNS。

## 安装前

1. 用 `docker version` 与 `docker compose version` 确认 daemon 和 Compose v2。
2. NAS 通过 DHCP 保留或静态配置拥有稳定的局域网地址；在 .env 填 NAS_IP。
3. 检查 TCP／UDP 53 和 TCP 8080 的现有占用，运行 `sh deploy/preflight.sh` 辅助诊断。
4. Docker named volumes 位于 NAS 本地存储，不使用 NFS／SMB 作为 SQLite WAL 数据目录。
5. 镜像运行 UID/GID 为 10001:10001。默认新命名卷继承镜像 /data 的所有权；若替换成 bind mount，预先给目录匹配的读写权限，勿把整个 NAS 目录授权给容器。
6. 运行 keygen 生成随机服务密钥，两个容器使用同一值；保留 .env，不提交到 Git。
7. `docker compose config --quiet` 通过后启动，`docker compose ps` 应显示两个 healthy。
8. IPv6 DNS 接入需另行验证 NAS 监听、Docker 网络、网段及端口；初次验证可先用 IPv4 DNS 地址，服务仍独立处理 AAAA 记录。

默认从 `ghcr.io/leoskey/dns-assistant:latest` 拉取统一双架构镜像，Docker 自动匹配 amd64／arm64；无需在 NAS 编译源码。可用 `DNS_ASSISTANT_IMAGE` 固定版本或摘要。源码构建使用 `-f compose.yaml -f compose.build.yaml`，之后的启动／重启等命令保持同一组覆盖文件。

更新时保留原项目名称、服务密钥和两个卷，执行 `docker compose pull`、`docker compose up -d --wait`。已有 fnOS 部署使用 `settings.env` 时保留其 `env_file`，仅替换两个服务的 image，并移除旧的 `pull_policy: never`。发布与匿名拉取检查见 [镜像发布维护](image-release.md)。

## 接入

浏览器打开 http://NAS_IP:8080，无需登录。管理仅用于受信任局域网，勿转发管理端口至公网。主机名访问使用 ADMIN_HOSTS 允许列表；NAS 反向代理应限制来源，不把所有转发来源当作外网访问的保护措施。

游戏主机将 DNS 设置为 NAS_IP，重新运行网络测试，观察“接入检查”中的真实来源与域名。Windows Docker Desktop 的转发可能合并来源，不能据此声称已识别每台主机。

有下载节点拒绝测试内容时，查看平台页与历史错误，不强制绑定该 IP。Xbox com／cn 不互换 IP；Nintendo 的联网测试地址与游戏下载不同，实际下载域名必须提供对应有效样本。

## 常见情况

| 现象 | 处理 |
| --- | --- |
| 53 端口占用 | 确认现有服务；不强制结束别的程序。可以为容器规划独立 IP，按 NAS 型号验证 Macvlan 支持及宿主机通信限制 |
| 管理入口 403 | 检查访问来源网段、URL 中的 Host 与 ADMIN_HOSTS；默认直接使用 NAS 的局域网 IP |
| DNS 核心尚未连接 | 检查两个容器 healthy、SERVICE_TOKEN 一致、内部网络与日志；策略定期重传 |
| 预算不足 | 适当增加预算或延长吞吐间隔；不会把未检查的节点刷新为有效 |
| 403／404／内容不足 | 目标 CDN 或样本不可用；保留普通解析，替换真实同域名内容样本再测 |
| 没有主机查询 | 确认主机 DNS、访客网络隔离、NAS 防火墙和端口发布 |
| IPv6 绕过预期节点 | 在界面分开看 A／AAAA；检查客户端是否用其他 DNS、HTTPS／SVCB 地址提示或旧连接 |
| 数据卷不可写 | 检查 UID/GID 和本地文件系统；修复后重启，避免使用 privileged 绕过 |
| 停止或升级 NAS | 先确认其他解析路径或把主机 DNS 改回自动；升级前导出配置并保留两个数据卷 |

## 实机记录

按 [验收矩阵](acceptance.md) 填写 NAS 型号／系统／架构、家宽运营商、路由模式、主机型号、IPv6、测试内容、时段、默认 DNS／优选 DNS 多轮完成时间与失败次数。Docker Desktop 通过不能代替该记录。
