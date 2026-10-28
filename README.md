# DNS Assistant

[![Docker images](https://github.com/leoskey/dns-assistant/actions/workflows/docker.yml/badge.svg)](https://github.com/leoskey/dns-assistant/actions/workflows/docker.yml)

[产品官网](https://leoskey.github.io/dns-assistant/)

当前源码版本：**v0.4.0**。管理页面“设置”中可查看正在运行的版本，命令行使用 `dns-assistant version`。

支持 Docker 部署与 Windows x64 单机安装的家庭游戏主机 DNS 优选工具。针对配置的 Xbox、PlayStation 和 Nintendo Switch 域名，在服务主机上测试候选公网 IPv4 的持续下载速度，并将合格节点用于后续 DNS 应答。

提供中文管理页面，局域网直接访问，无需登录，也不需要安装 AdGuard Home。项目优化的是已配置域名的节点选择；实际游戏下载效果取决于规则、样本和家庭网络。

当前候选加入下载代理、连接记录与双模式接入界面，尚未公开发布。下载代理仅向配置的局域网开放 TCP 8082；升级前冷备份完整数据卷，schema 4 回退到旧程序须恢复匹配备份。详见 [下载代理](docs/download-proxy.md)。

## 主要功能

- **支持自定义下载域名**：规则按 Xbox、PlayStation 或 Switch 分类，测试地址不限制平台域名后缀；继续校验公网 IPv4、HTTP(S) 标准端口及内容。
- **代理实时下载速度**：首页每秒采样经过代理的全部主机下行流量合计，显示 MB/s；后台探测与 DNS 直连流量不计入。

- **按域名自动选优**：候选来自规则配置、上游 DNS 的 A 记录和当前节点；各域名独立测试，保持原 Host／SNI 并校验证书。
- **按下载速度排名**：完整测速按时间窗口执行，不限速、不设字节上限或每日流量预算；速度接近时保留当前节点，减少频繁切换。
- **低频复测与故障回退**：默认每 24 小时完整复测、每 30 分钟检查当前节点；调度持久化，重启后不重复刚完成的测试。
- **规则订阅**：在后台填写一个 HTTPS JSON 地址，默认每 6 小时同步；完整校验后替换规则，同步失败保留上次可用配置。
- **管理与记录**：支持接入检查、立即复测、恢复默认、暂停选优、规则编辑和配置备份恢复。查询、测速及事件记录分页显示并自动清理，每日汇总有界保留；设置保存按钮固定在右下角。
- **独立 DNS 服务**：提供 IPv4 UDP／TCP DNS 和普通解析缓存。管理容器停止后，DNS 容器继续使用有效策略，策略过期后恢复默认解析。

完整测速会占用带宽并产生实际下载流量。默认调度为低频执行，可在设置中调整或暂停自动选优。

## 工作方式

1. 主机向服务主机查询 DNS。没有有效优选时，使用普通上游解析，查询不等待测速。
2. 后台收集已配置域名的合法候选，用该域名的样本逐个检查内容并测量持续下载速度。
3. 完整测试结束后选择合格节点，发布给 DNS 服务。连接延迟不参与速度排名，正常复测期间继续使用有效旧优选。
4. 健康检查失败时，先撤销该域名的覆盖、恢复默认解析，再进行完整复测；通过后重新应用优选。
5. DNS 更新影响后续解析和新连接，已经建立的下载连接不会因此重新连接。

这里的“优选”表示本轮已验证候选中的选择，不代表找到了整个 CDN 的全部 IP，也不代表主机已经获得相同比例的提速。

## 快速部署与接入

可选择 **Docker 部署**或 **Windows 单机安装**。Docker 使用下面的 Compose 流程；Windows 使用安装 EXE 或便携 ZIP，内置管理页面，无需 Docker 与开发环境，具体见 [Windows 安装与升级](docs/windows.md)。两种方式均需要固定局域网 IPv4、TCP／UDP 53 和 TCP 8080。Windows 单机程序退出时 DNS 与管理一同停止。

### 环境要求

- 支持 Docker Engine 与 Compose v2 的服务主机，使用固定局域网 IPv4。
- 服务主机的 TCP／UDP 53 和 TCP 8080 可用，主机可以访问这些端口。
- 数据卷位于服务主机本地文件系统，部署机器能够下载构建所需的基础镜像和依赖。

下面使用预构建镜像，`.env.example` 固定本次发布版本；无需在部署机器安装构建工具。源码构建与版本核对见 [镜像发布维护](docs/image-release.md)。

### 安装

```sh
git clone https://github.com/leoskey/dns-assistant.git
cd dns-assistant
cp .env.example .env
```

Linux 服务主机保持 `.env` 中的 `SERVER_IP` 留空，以下命令会自动检测局域网出口地址；需要指定其他网卡时再填写该项。然后拉取镜像并生成服务密钥：

```sh
sh deploy/compose.sh pull
sh deploy/compose.sh run --rm --no-deps controller keygen
```

将生成的密钥填入 `.env` 的 `SERVICE_TOKEN`，替换示例占位值。该密钥用于两个服务之间的通信。随后检查并启动：

```sh
sh deploy/compose.sh config --quiet
sh deploy/compose.sh up -d --wait
sh deploy/compose.sh ps
```

脚本会输出检测到的服务地址。两项服务应显示为 `healthy`；在浏览器访问 `http://检测到的地址:8080`。Windows 用户使用 [安装程序或便携版](https://github.com/leoskey/dns-assistant/releases/tag/v0.4.0)。

自动检测脚本在 Linux 宿主机上运行，需要 iproute2 的 `ip` 命令；Windows 单机程序直接发现本机地址。远程 Docker、macOS／Docker Desktop 或无法确认局域网出口时，在 `.env` 显式填写 `SERVER_IP` 后使用原有 `docker compose` 命令。地址仍应通过 DHCP 保留或静态配置保持稳定。

管理页面免登录，只应在受信任局域网开放。使用服务主机主机名或反向代理时，将允许的主机名加入 `.env` 的 `ADMIN_HOSTS`；不要将管理入口直接暴露到公网。

### 连接游戏主机

1. 在首页查看 PS5／PS4、Xbox 或 Switch 的 DNS 设置指引，将主 DNS 设置为服务主机的 IPv4。
2. 在“接入检查”查看实际查询来源，确认请求到达；也可检查指定域名的 DNS 应答。
3. 在“平台选优”查看当前节点、最近测速、下次计划和失败原因，需要时使用“立即复测”或“恢复默认”。

先接入一台主机，实际下载验证后再接入其他设备。服务不会自动修改路由器、DHCP 或全家设备的 DNS。

公共备用 DNS 可能被主机并行使用，从而绕过选优。服务主机停机、升级或停止服务前，先将主机 DNS 恢复自动或切换到其他可用服务；平台操作与故障处理见 [部署说明](docs/deployment.md)。

## 适用范围与限制

- **规则只作用于精确域名。** 默认内置 Xbox 全球／国内域名、PlayStation 下载与测速样本、Nintendo 连接测试样本；内置不代表已在每个家庭网络中验证，也不代表覆盖平台全部下载域名。
- **样本必须适用于当前域名。** 节点需返回有效内容且样本足够覆盖测速窗口。样本失效、需要设备认证或签名过期时，应更换可用样本，不能强行视为测速成功。
- **连接测试与游戏下载分别判断。** Switch 的 `ctest` 等连接测试成绩不能证明实际游戏下载更快；实际下载域名需要对应的有效内容样本。
- **服务主机测速不是主机实时速度。** DNS 记录只能说明收到了哪些查询，不能显示游戏下载进度、HTTPS 内容或实际下载带宽。经路由器转发时，查询来源可能只显示路由器。
- **仅优选公网 IPv4。** 启用规则的域名 AAAA 返回 NODATA，其他域名 AAAA 保持上游语义。HTTPS／SVCB 地址提示可能影响客户端选址；要求端到端 DNSSEC 的 A 查询保留原始上游应答。
- **用途为下载节点选择与手动代理转发。** 支持 HTTP 下载及 HTTPS CONNECT 隧道，不提供 Xbox 国际地址转国内地址的 HTTP 跳转、透明接管或对战网络加速。

既有验证覆盖自动化、Linux/amd64 容器及 amd64 部署运行；Linux/arm64 在选优重构后仅完成编译检查。三平台真实主机下载收益、ARM 服务主机运行和宿主机重启仍待实测。验证结论不表示任一设备当前已升级，范围与复现方法见 [验证记录](docs/validation.md)。

## 维护与文档

在“设置”中维护平台、测试下载地址和启用状态，候选 IP 与内容校验位于高级选项。订阅更新会整体替换规则，包括本地手工修改；表格式、同步与关闭方式见 [规则订阅](docs/rule-feed.md)。

升级前保留 `.env` 和两个完整数据卷；界面导出的配置不包含历史、每日汇总及调度时间，不能代替卷备份。将 `.env` 中的 `DNS_ASSISTANT_IMAGE` 更新为目标发布版本后拉取并启动：

```sh
sh deploy/compose.sh pull
sh deploy/compose.sh up -d --wait
```

`sh deploy/compose.sh down` 默认保留数据卷；`down -v` 会删除数据。回滚到旧数据库格式的程序时，需要同时恢复匹配的数据卷快照。具体升级、回滚和日志检查方法见 [部署说明](docs/deployment.md)。

开发需要 Go 1.27.2 和 Node.js 24，前端构建后嵌入 Go 程序：

```sh
npm ci --prefix web
npm run build --prefix web
npm test --prefix web
go test ./...
go vet ./...
go build -o bin/dns-assistant ./cmd/dns-assistant
```

- [需求与行为说明](docs/requirements.md)
- [验收标准](docs/acceptance.md)
- [验证记录与浏览器测试方法](docs/validation.md)
- [实现与技术选型](docs/architecture/technology-selection.md)
- [管理 API](docs/api.md)
- [镜像发布维护](docs/image-release.md)
- [Windows 单机安装与打包](docs/windows.md)
- [产品官网开发与验证](docs/website.md)
- [第三方组件许可](THIRD_PARTY_NOTICES.md)

样本来源参考 [XboxDownload 测试配置](https://github.com/skydevil88/XboxDownload/blob/master/XboxDownload/Models/SpeedTest/SpeedTestDataBuilder.cs)。本项目独立实现 DNS 选优业务逻辑，第三方组件的许可说明随源码与镜像提供。
