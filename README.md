# DNS Assistant

[![Docker images](https://github.com/leoskey/dns-assistant/actions/workflows/docker.yml/badge.svg)](https://github.com/leoskey/dns-assistant/actions/workflows/docker.yml)

在 NAS 上用 Docker 部署的家庭游戏主机 DNS 服务。面向中国大陆家宽，支持 Xbox、PlayStation、Nintendo Switch 的已配置下载域名；运行时自动测量候选节点、选择并应用 IP，失败时回到普通解析。

管理界面使用 React，采用 shadcn/ui neutral 风格与 Lucide 图标，局域网直接访问，无需登录。产品独立运行，不需要 AdGuard Home。DNS 基础使用已锁定的 dnsproxy 库，下载吞吐选优由本项目实现。

## 已实现

- UDP／TCP DNS、上游缓存、A／AAAA 独立策略、精确域名匹配、超时回退。
- 启动与运行期间自动探测；HTTP 内容检查、吞吐采样、近期成功率与中位数排名、切换迟滞、备用节点、固定节点验证。
- 持久化每日预算，保留健康检查额度；取消、时间与字节上限。
- 双容器隔离，版本化策略与独立快照；管理停止时 DNS 继续服务，策略过期恢复普通解析。
- React 总览、平台、接入检查、历史、可视化设置与规则维护、配置备份恢复及 SSE。
- 首页显示可复制的 NAS DNS 地址及 PS5／PS4、Xbox、Switch 设置指引；页面与操作提供轻量动画，支持减少动态效果。
- 平台 Tab 与节点列表同时展示 IPv4／IPv6，支持一键测速；下载规则使用列表展开维护。接入页直接检查实际 DNS 应答并展示最近查询，无需登记主机。
- 局域网来源／Host 限制、跨站写入检查，内部服务接口独立密钥校验。

## NAS 部署

需要 Docker Engine 与 Compose v2。先为 NAS 保留固定局域网 IP，检查宿主机 TCP／UDP 53 和 TCP 8080 可用。默认数据卷应位于 NAS 本地文件系统。

预构建镜像为 `ghcr.io/leoskey/dns-assistant:latest`，同时支持 `linux/amd64` 和 `linux/arm64`，Docker 自动选择 NAS 对应架构。默认 Compose 直接拉取镜像。

中国大陆访问 GHCR 缓慢时，可在 `.env` 将 `DNS_ASSISTANT_IMAGE` 改为 `ghcr.nju.edu.cn/leoskey/dns-assistant:latest`，通过南京大学 GHCR 缓存拉取相同发布内容。本项目已验证该路径的 NAS 冷下载与实际重建，详见 [NAS 拉取加速与升级](docs/image-release.md#nas-拉取加速与升级)。

```sh
git clone https://github.com/leoskey/dns-assistant.git
cd dns-assistant
cp .env.example .env
# 编辑 NAS_IP，填写 NAS 的固定局域网 IPv4
docker compose pull
docker compose run --rm --no-deps controller keygen
# 把生成的值填入 .env 的 SERVICE_TOKEN，替换示例占位值
docker compose config --quiet
docker compose up -d --wait
```

在浏览器访问 `http://NAS_IP:8080`。主机网络设置中将 DNS 指向 NAS_IP，再在“接入检查”确认请求确实到达。先接入一台主机，实际下载通过后再推广到其他设备。服务不会修改路由器、DHCP 或全家 DNS 设置。

管理页免登录，应仅在受信任局域网开放。若使用 NAS 主机名／反向代理，把该主机名填到 .env 的 ADMIN_HOSTS；未明确允许的域名 Host 会被拒绝。需要 IPv6 DNS 接入时，另行配置 Docker IPv6 网络与监听、访问网段及端口发布，不能把“支持 AAAA 应答”当作 IPv6 接入已验证。

```sh
docker compose ps
docker compose logs --tail 100
docker compose restart
docker compose down
```

`down` 保留配置与历史卷；勿在希望保留数据时使用 `down -v`。停止 NAS DNS 前，请把主机 DNS 改回自动或另一台可用实例。第二 DNS 可能被客户端并行使用，填写公共 DNS 会降低选优覆盖的确定性。

更新镜像时，先导出设置并保留 `.env` 与两个数据卷，再运行：

```sh
docker compose pull
docker compose up -d --wait
```

`.env` 的 `DNS_ASSISTANT_IMAGE` 可改为已发布版本标签或 `ghcr.io/leoskey/dns-assistant@sha256:...`，用于固定版本和回滚。现有 NAS 若使用 `settings.env`，继续保留原密钥与卷；在 Compose 中更新两个服务的 image 即可，不要重新生成密钥或创建空卷。

## 自动发布

[Docker images 工作流](https://github.com/leoskey/dns-assistant/actions/workflows/docker.yml) 在前端构建、Vitest、Go race 测试与 vet 通过后发布 GHCR 镜像，无需额外保存 registry 密码。

| 触发 | 镜像标签 |
| --- | --- |
| 推送 main／在 main 手动运行 | `latest`、`sha-完整提交号` |
| 推送 `vX.Y.Z` 标签 | `X.Y.Z`、`X.Y`、`sha-完整提交号`；不改动 `latest` |
| main 的 Pull Request | 仅运行验证，不发布 |

每次发布包含两种架构的统一 manifest、源码与提交标签、构建来源信息；工作流实际运行两个架构的配置命令。摘要记录可用于固定镜像的 digest。本项目镜像已公开并通过匿名拉取验证；fork 创建的新包首次发布后需在包设置中设为 Public，详见 [镜像发布维护](docs/image-release.md)。

## 下载规则与适用范围

默认内置 Xbox 全球／国内下载域名、PlayStation 样本和 Nintendo 联网检测样本。每条规则只在自己的域名与样本验证通过后生效；样本失效或节点拒绝请求会保留上游解析。内置域名不代表已在用户家宽通过验证。

Nintendo 的联网测速与实际游戏下载域名不同。对实际游戏下载，需要在“设置”添加对应下载域名及可访问的真实内容样本；缺少样本、需要设备认证或签名过期时，不用联网检测成绩冒充游戏下载收益。同样适用不同区域、冷门内容和新增平台域名。

“最优”是在预算内验证过的候选中的当前选择。NAS 探测不是主机实时下载速度；DNS 改变只影响后续解析和连接。HTTPS／SVCB 地址提示及要求端到端 DNSSEC 的请求保留上游语义。Xbox HTTP 国内下载跳转不属于 DNS 首版模块。

样本与域名参考 [XboxDownload 测试配置](https://github.com/skydevil88/XboxDownload/blob/master/XboxDownload/Models/SpeedTest/SpeedTestDataBuilder.cs) 及其域名清单；本项目独立实现业务代码，实际内容读取有上限。

## 开发与验证

Go 1.27.2、Node 24。依赖锁定在 go.mod／go.sum 和 web/package-lock.json。前端由 Go 嵌入，因此先构建 Web 再执行 Go 测试或编译。

```sh
npm ci --prefix web
npm run build --prefix web
npm test --prefix web
go test ./...
go vet ./...
go build -o bin/dns-assistant ./cmd/dns-assistant
docker build --target test -t dns-assistant:test .
docker buildx build --platform linux/arm64 -t dns-assistant:arm64 --load .
```

需要在 NAS 或开发机从源码构建时，使用独立覆盖文件：

```sh
docker compose -f compose.yaml -f compose.build.yaml build
docker compose -f compose.yaml -f compose.build.yaml up -d --wait
```

后续对源码部署的操作继续带上这两个 `-f` 参数，避免切回预构建镜像。

本机双进程开发可分别设置 SERVICE_TOKEN、DATA_DIR、LISTEN_ADDR、CONTROL_ADDR、DNS_CONTROL_URL、MANAGER_INTERNAL_URL、WEB_ADDR、INTERNAL_ADDR；服务间使用同一随机密钥。默认使用 DNS 5353、控制 9090、Web 8080、观察 8081，均可用环境变量调整。前端开发代理默认指向 127.0.0.1:8080。

- [完整验收标准](docs/acceptance.md)
- [验证结果与待实测项目](docs/validation.md)
- [当前需求](docs/requirements.md)
- [实现与技术选型](docs/architecture/technology-selection.md)
- [部署检查与故障处理](docs/deployment.md)
- [镜像发布维护](docs/image-release.md)
- [管理 API](docs/api.md)
- [第三方组件许可记录](THIRD_PARTY_NOTICES.md)

NAS／真实主机的下载收益须按验收标准实测，容器和自动化通过不代替现场验收。
