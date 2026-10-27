# 当前版本验证记录


更新：2026-10-27。v0.2.0 下载代理完成以下软件与本地容器检查，未发布或升级既有部署。规格见 [需求](requirements.md)、[验收矩阵](acceptance.md) 和 [实施计划](implementation-plan.md)。后文的 2026-10-20 DNS 基线与 NAS 部署记录只代表当时的版本。

## 当前界面集成验证

已集成 `preview/codespaces` 的 `0001116`：节点列表与新增弹窗、解析检查弹窗、独立运行日志、左下角版本入口及默认每页 10 条记录。下载代理和通用 SERVER_IP 部署保持可用；恢复默认、固定节点与每日汇总仍由 API 提供。

- 全套 Go 竞态测试与 vet、前端生产构建及 2 项 Vitest 通过。
- Chromium 对真实 Go／SQLite／DNS／HTTP 夹具检查六页与五种窗口尺寸；覆盖节点平台 Tab、Radix Select、新增时保留配置、解析弹窗、分页与记录 Tab、规则同步、代理启停／实际转发／复制及固定保存区。
- 新增节点测试主动移除浏览器 randomUUID，确认普通 HTTP 缺少该 API 时仍可保存。规则 ID 沿用现有生成方式。
- 默认／源码 Compose 与 Codespaces 预览 Compose 配置校验通过，linux/amd64 镜像构建及本地隔离双容器预览启动、策略同步通过。没有启动远端 Codespaces 或对真实主机做下载验收。

本机集成证据在忽略的 `artifacts/merge-check/`，不随克隆提供。Windows 运行交叉编译的测试程序时，需要可用的 IANA 时区数据库；未安装 Go 的环境可将 ZONEINFO 指向 Go 的 `lib/time/zoneinfo.zip`。现有浏览器脚本覆盖上述交互，不以静态构建代替运行检查。

## v0.2.0 下载代理基线验证

| 项目 | 结果与范围 |
| --- | --- |
| Go 回归与 vet | 50 项顶层测试通过，含 6 项代理测试及其子场景；浏览器夹具在普通运行中跳过，单独端到端运行通过；全包 vet 通过 |
| HTTP 转发 | 本地真实 HTTP 服务验证 Host、签名查询参数、Range／206、HEAD、流式字节、代理凭据和逐跳头清理、目标连接失败 502 |
| HTTPS CONNECT | 本地真实 TLS 服务验证双向内容、原始 SNI／Host、有效证书；错误域名和不受信任证书仍被客户端拒绝；原始 TCP 验证随 CONNECT 一起发送的缓冲数据及退出清理 |
| 策略与安全 | 有效／过期节点、暂停、固定、禁用规则、普通域名、优选 TCP 失败回退；非允许来源、私网／环回／特殊 IP、IPv6、非标准端口及私网 DNS 结果被拒绝；并发上限、环境代理隔离通过 |
| 配置与兼容 | 默认关闭；保存、重启加载、备份恢复、旧配置缺字段保持关闭；SERVER_IP 优先、旧 NAS_IP 兼容、无效声明不猜测地址 |
| 浏览器 | Chromium 使用真实 Go／SQLite／DNS／HTTP 夹具，代理启用保存与重载、实际请求转发及流量、复制地址、PS／Switch 指引、Xbox DNS 提示、关闭后拒绝新请求通过 |
| 布局 | 五页覆盖 1280×800、1024×320、360×640、360×480、320×568，无页面横向溢出，保存区和服务状态无遮挡；代理配置与步骤另检查 360×640、320×568、1024×320，并检查桌面与手机截图 |
| 前端与版本 | TypeScript／Vite 生产构建、2 项 Vitest、源码／前端包／锁文件／README 的 0.2.0 版本一致性通过 |
| 编译 | Windows/amd64、CGO_ENABLED=0 的 Linux/amd64 与 Linux/arm64 通过；arm64 未运行 |
| Linux 竞态 | Dockerfile test 阶段执行 go test -race ./...，全包通过 |
| 容器 | linux/amd64 镜像构建；隔离的非 root、只读根目录双容器启动、健康检查、策略同步、实际内部 DNS 查询、代理默认关闭／启用后拒绝私网目标、重启保留代理设置通过；使用临时数据，最终检查关闭自动公网探测 |
| Compose 与仓库边界 | 默认及源码覆盖 Compose 校验、SERVER_IP 优先和旧变量回退通过；截图、二进制、测试数据库和报告在忽略目录，暂存区无本机产物 |

本机证据保存在忽略的 `artifacts/proxy-check/`：`go-test.jsonl`、`browser/report.json`、界面截图及 `runtime-report.json`；不随仓库克隆提供。代理协议夹具与浏览器脚本在源码中，可按后文命令重新运行。HTTP／TLS 成功转发由受控本地协议测试证明，容器检查不冒充公网 CDN 或真实主机下载验收。运行容器检查需指定 DATA_DIR=/data 并加载暂停探测的临时配置；使用随机宿主端口时，重启后重新读取 docker port。

未验证：PlayStation／Switch 真机下载与提速收益、Xbox 代理接入、目标物理宿主机重启、ARM 设备运行。未执行发布、推送或现有设备升级。

## 2026-10-20 DNS 基线检查

| 项目 | 结果与范围 |
| --- | --- |
| Go 全套测试 | 43 项顶层测试通过，另含 HTTP、地址与安全子场景；浏览器夹具在普通运行中主动跳过，单独端到端运行通过 |
| Go vet | 全包通过 |
| 前端 | TypeScript/Vite 生产构建及 2 项 Vitest 通过 |
| 本机编译 | Windows/amd64 通过 |
| 多架构编译 | CGO_ENABLED=0 的 Linux/amd64、Linux/arm64 通过；amd64 已在 Linux 容器运行，arm64 仅编译 |
| Linux 竞态检查 | Dockerfile 的 test 阶段执行 go test -race ./...，全包通过 |
| 发布镜像与双容器 | linux/amd64 镜像构建通过；非 root、只读根文件系统运行，DNS/controller 健康与策略同步通过；schema 1 夹具迁移到 3、历史事件保留、分页、数据库完整性与双容器重启保留设置通过 |
| Compose | 使用 .env.example 执行 config --quiet 通过，包括日志轮换配置 |
| 浏览器 | Chromium 连接真实 Go 管理 API、SQLite、DNS 核心及受控 HTTP 下载/规则服务，通过实际 DNS 检查、自动选优、固定验证、恢复默认、复测、健康失败回退与恢复 |
| 同步与编辑 | 坏规则表保留旧配置、有效规则表事务更新、配置验证失败后重试、新增规则及高级字段保留通过；模拟策略同步失败时界面显示等待同步，恢复后才显示实际状态 |
| 记录 | 查询、测速、事件分页/快照/筛选与写入数量上限、启动/定时清理、每日汇总与 90 天/10,000 行保留通过 |
| 布局与键盘 | 五页覆盖 1280×800、1024×320、360×640、360×480、320×568；设置顶部/底部保存按钮可见，正文/状态栏无遮挡，无页面级横向溢出；平台 Tab 方向键、Home、End 通过 |

## 后端重点回归

- 全部 9 个候选参赛，晚出现且连接较慢的快节点可以入选；拒绝 IPv6/私网地址；速度接近保持，明显更快单轮切换。
- 实际读取超过旧的 8 MiB 限制；连接与预热不计入持续速度，短样本、TLS、内容、取消和超时处理通过。
- 默认 → 优选 → 健康失败先发布默认 → 完整复测恢复的实际本地 DNS/HTTP 集成通过；普通域名 AAAA、缓存、CNAME、否定应答、EDNS、TCP、DO/CD 边界继续通过。
- 正常复测保持当前节点；恢复默认隔离旧结果，取消中的候选发现不会覆盖新调度，其他域名任务保留；健康任务可在候选之间优先执行。
- 低频调度重启保留、恢复失败退避；旧格式备份、schema 1 升级到 3、预算表清除、历史保留、拒绝未来版本与写入失败回滚通过。
- 规则表私网目标、重定向、超大/损坏内容、超时、重复同步、下载期间设置变化和数据库不可写均保留原可用规则；内容完全未变的同步不取消队列工作。

## 复现

```sh
npm ci --prefix web --no-audit --no-fund
npm run build --prefix web
npm test --prefix web
go test ./...
go vet ./...
go build ./cmd/dns-assistant
```

浏览器检查脚本为 [scripts/browser-check.cjs](../scripts/browser-check.cjs)，配套夹具在 [browser_test.go](../internal/app/browser_test.go)。先执行 `go test -c -o ../work/app.test.exe ./internal/app`（其他系统可不使用 .exe 后缀），然后设置 APP_TEST_BINARY 为该文件的绝对路径、PLAYWRIGHT_MODULE 为可用 Playwright 模块路径、CHROME_PATH 为 Chromium/Chrome 可执行文件路径、BROWSER_OUTPUT_DIR 为证据目录，再运行 `node scripts/browser-check.cjs`。脚本只启动测试二进制；夹具接口不包含在生产程序中。

## 2026-10-20 NAS 部署验证

- 现有 fnOS amd64 项目采用离线镜像 `dns-assistant:2026.10.20-optimization-r2`，保留原项目名、SERVICE_TOKEN、settings.env、端口和两个命名卷。两服务使用 pull_policy: never，日志轮换为 10 MiB × 3。
- 在停止两个原服务后，以只读方式挂载两个数据卷归档；备份原 Compose/env，归档可读性校验通过，SHA256SUMS 和 COMPLETE 已生成。备份位于原项目的 `backups/pre-optimization-20261020/`。一次性备份容器已退出且不自动重启；原镜像保留。
- NAS 管理健康、DNS 核心连接与策略版本同步通过；13 条原规则完全一致，300 条升级前历史抽样逐条保留。查询、测速、事件、每日汇总分页及保留上限通过。
- 从局域网电脑对 NAS 53 端口执行 16 项实际查询：UDP/TCP × 普通域名及三平台样本 × A/AAAA。所有应答成功；目标 AAAA 为 NODATA，普通域名 AAAA 正常转发。
- 最终镜像重建后，所有最近复测和下次计划与重建前完全一致，没有重复触发已完成测试。浏览器核对新总览文案、平台当前节点与下一日计划、记录第二页、设置固定保存区；页面使用 `index-r3V6soWC.js` 和 `index-D90uMuq3.css`。

## 验收边界

上述 NAS 记录适用于 2026-10-20 的 DNS 离线镜像，不证明 v0.2.0 下载代理已部署。历史合成夹具的 SQLite integrity_check 已通过；现场验证使用 API/DNS 和历史抽样，未直接对生产 SQLite 执行 integrity_check。真实主机下载收益、物理宿主机重启和 ARM 运行仍需对应设备验收。后台测速及代理累计字节均不等同于真实游戏下载完成时间。

[重构前的 NAS 与 CI 记录](history/pre-optimization-validation.md) 仅为历史证据。退役的预算、双地址族选优、两轮切换与备用激活不再作为当前标准。
