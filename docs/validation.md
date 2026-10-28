# 当前版本验证记录

初始验证批次：2026-10-20 的选优重构。文档整理：2026-10-28。原有表格与后端、amd64 部署结论为既有记录，不表示整理时重新运行过测试，也不声明具体设备的当前版本；增量验证按日期单独列出。规格见 [需求](requirements.md)、[验收矩阵](acceptance.md) 和 [实施计划](implementation-plan.md)。

## v0.3.1 发布验证（2026-10-28）

- [版本标签流水线](https://github.com/leoskey/dns-assistant/actions/runs/40620988789)、[主线发布复验](https://github.com/leoskey/dns-assistant/actions/runs/40621542710) 和 [官网部署](https://github.com/leoskey/dns-assistant/actions/runs/40621542863) 全部成功；发布源码为 `8fcb571f9c48460a34a279026b801b914b795847`，保留 v0.3.0 的功能与 `SERVER_IP` 配置。
- [公开 Release](https://github.com/leoskey/dns-assistant/releases/tag/v0.3.1) 的安装 EXE、便携 ZIP 和校验清单已实际匿名下载；两个包的 SHA-256 同时匹配清单和 GitHub 附件元数据，安装器及解压后程序均为 v0.3.1，程序 revision 与发布源码一致，`config` 烟测通过。安装／卸载和数据保留测试由 Windows CI 执行。
- 使用空 Docker 凭据目录匿名拉取 Linux amd64／arm64 镜像，两种架构的 CLI、OCI 版本和配置烟测通过。验证时 `0.3.1` 与 `latest` 指向同一 manifest；ARM 为模拟运行，不作为实机验收。
- [官网](https://leoskey.github.io/dns-assistant/) 页脚、更新记录、文档及下载入口均为 v0.3.1，两个下载链接最终 HTTP 200。安装包仍未代码签名，真实主机下载收益及 ARM 实机待验收。
- 完整版本、镜像摘要、附件哈希、验证范围和复现步骤见 [v0.3.1 发布验证报告](releases/v0.3.1-validation.md)。本机原始产物不随克隆提供。

## 已执行的检查

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

## amd64 部署验证结论

- 升级前完整数据卷归档的可读性校验通过；升级后配置一致、历史抽样保留，管理健康、DNS 核心连接与策略版本同步正常。
- 局域网 IPv4 上的 UDP／TCP 查询覆盖普通域名与三平台样本的 A／AAAA，应答符合规格：目标 AAAA 为 NODATA，普通域名 AAAA 正常转发。
- 容器重建后最近复测与下次计划保留，没有重复触发已完成测试；查询、测速、事件和每日汇总分页及保留上限通过。
- 浏览器核对总览、当前节点与下次计划、记录翻页和固定保存区通过。

上述结论仅覆盖既有 amd64 部署检查。设备标识、运行配置、业务数据、镜像包标识、备份位置及原始日志／截图不随仓库提供。目标环境按 [部署说明](deployment.md) 与 [空白验收模板](field-validation-template.md) 复验，填写后的记录保存在仓库外。

## Docker 与 Windows 单机交付（2026-10-28）

- Go 全套测试及 `go vet ./...` 在 Linux 构建容器通过，测试开启 `-race`。同一源码交叉编译的 Windows 管理命令测试与业务包测试在 Windows 原生运行通过；普通运行继续跳过浏览器专用夹具。
- 新增生命周期测试使用临时 SQLite 与本地 DNS／HTTP：内嵌管理 HTML、内部策略连接、目标 AAAA 的 UDP／TCP NODATA、正常取消、重启保留暂停配置和管理端口冲突通过。所有部署烟测预先暂停自动选优，不使用真实主机或公网测速收益作为夹具。
- Windows x64 安装 EXE、便携 ZIP、SHA-256 清单实际生成；校验清单、便携程序 `version`／`config`、安装、开始菜单目标与独立数据参数、同版本重装、卸载与未跟踪数据保留检查通过。包内含使用说明与第三方许可。测试脚本为 `scripts/test-windows-package.ps1`；安装测试需要无既有安装的用户环境。
- 最终 Windows 程序在隔离临时目录实际运行，管理接口、UDP／TCP 健康应答、配置保存及进程重启后的暂停／时区配置保留通过。无本机 Go 环境下保存 `Europe/London` 成功，Windows 程序内置 IANA 时区库。测试程序与最终包内 EXE 的 SHA-256 一致。
- 浏览器实际打开最终 Windows 程序的管理页，显示 DNS 与管理服务连接、策略同步及暂停状态；修正未设置地址和断连提示，使其同时适用于单机与容器部署。TypeScript／Vite 生产构建与 2 项 Vitest 回归通过；未重跑此前完整浏览器交互矩阵。
- 当前 Linux/amd64 Docker 镜像构建通过，默认与源码覆盖 Compose 校验通过；独立测试项目使用新卷、只读根目录、非 root 运行，两项健康检查与策略同步通过。UDP／TCP 应答、双容器重启、镜像重建后暂停配置保留，以及停止 controller 后 DNS 继续应答通过；结束后移除测试容器、网络和测试卷。
- 打包及证据位于忽略目录 `artifacts/`，不随源码克隆提供。安装包暂未代码签名；本地构建使用 revision `unknown`，不对应已发布的版本提交。该初始本机批次没有远端发布记录；后续正式版本以相应标签的 Actions 与 Release 为准。
- 本批次未验证 Windows 10 干净虚拟机、53 标准端口的真实局域网入站、防火墙、开机启动、睡眠唤醒、真实主机下载或 ARM 运行。高位环回测试端口不代替主机接入验收。

复现与使用见 [Windows 单机安装](windows.md) 和 [Docker 部署](deployment.md)。

## 验收边界

### v0.3.0 命名与地址配置验证（2026-10-28）

- 当前维护文件的文本和文件名扫描通过，旧平台称谓已移除；地址变量在 Compose、单机入口、状态 API、自指上游校验、测试和说明中统一为 `SERVER_IP`。版本配置和官网链接同步为 v0.3.0，旧发布记录不改写。
- 默认及源码覆盖 Compose 配置校验通过；缺少 `SERVER_IP` 的配置被明确拒绝。前端生产构建、Vitest、官网生产构建与五档浏览器回归通过。
- Go 竞态测试通过；Windows 原生业务测试通过。复验暴露了测试只检查 TCP 端口的问题，已改为同时预留 TCP／UDP；初始化时的 SQLite 锁等待设置前移后，Windows 单机启动／停止／重启测试连续五次通过。
- 真实管理 API 和浏览器针对性检查通过：`SERVER_IP` 返回为 `dns_address`，首页显示同一地址，保存指向服务自身的上游被拒绝。检查使用临时数据库、DNS 和下载夹具。
- 原完整管理页浏览器脚本仍查找现行界面已删除的状态文案和详情控件，本次执行未通过，不能作为完整交互矩阵通过的证据；上述针对性检查不扩大为全界面验收。本机日志和截图保存在忽略目录。

### v0.2.0 发布验证（2026-10-28）

- [标签流水线](https://github.com/leoskey/dns-assistant/actions/runs/40607962487) 的应用测试、Windows 打包与原生安装测试、双架构镜像发布与检查、GitHub Release 全部通过。发布源码为 `593826d1f70e4fa642d8862f006e31df7d816401`。
- [公开 Release](https://github.com/leoskey/dns-assistant/releases/tag/v0.2.0) 的安装 EXE、便携 ZIP 和 `SHA256SUMS` 匿名重新下载成功，逐项校验 SHA-256；从下载的实际附件重跑安装、开始菜单参数、同版本重装、卸载与保留数据检查通过。Windows 二进制报告 v0.2.0 与上述源码 revision。
- `ghcr.io/leoskey/dns-assistant:0.2.0` 匿名 manifest 包含 Linux amd64／arm64。使用空 Docker 凭据目录按平台摘要实际拉取并运行两种架构的 `version`，均报告相同版本与 revision；arm64 运行于模拟器，不作为 ARM 实机验收。
- 镜像索引摘要：`sha256:e420a61925ad0e7f2da8673bc85ce3392db01837b0282ee8eeed92e122b2b86c`，与 Release 说明一致。源码、公开镜像和附件均可追溯；本地下载与测试产物保存在忽略目录。

### 实机验收边界

Linux/arm64 在该验证批次仅完成编译，未做 ARM 服务主机运行验收；重构前的模拟运行不扩大本批次的覆盖范围。合成夹具的 SQLite integrity_check 已通过，部署验证未直接对生产数据库执行该检查。三平台真实主机下载收益、晚高峰、休眠唤醒与宿主机重启仍待实测；服务端测速和本地测试数据均不能代表主机实时下载速度。

源码、公开镜像与设备版本分别核对，不通过历史部署记录判断 `latest` 是否包含某项改动；发布检查方法见 [镜像维护](image-release.md)。[重构前验证摘要](history/pre-optimization-validation.md) 仅为历史证据。退役的预算、双地址族选优、两轮切换与备用激活不再作为当前标准。
