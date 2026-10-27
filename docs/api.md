# 管理 API


更新：2026-10-27。管理界面无需登录，API 仅允许配置的局域网来源与允许的 Host。除 GET／HEAD 外必须提供 `X-DNS-Request: 1`；浏览器请求还必须同源，不提供跨源 CORS 授权。

| 方法与路径 | 行为 |
| --- | --- |
| GET /healthz | 管理数据库可用检查 |
| GET /api/v1/status | 自动状态、当前策略、任务、下次复测和同步状态，不携带记录列表 |
| GET /api/v1/config | 当前完整设置 |
| PUT /api/v1/config | 完整校验后事务替换设置，取消旧规则探测 |
| GET /api/v1/connections | 分页读取代理连接记录：时间、来源、方法、域名与请求地址 |
| GET /api/v1/queries | 分页查询接入请求记录 |
| GET /api/v1/history | 分页测速结果，支持 filter=all／throughput／health／failed |
| GET /api/v1/history/events | 独立分页的运行与切换事件 |
| GET /api/v1/history/daily | 独立分页的 UTC 每日速度、失败、流量与启用/切换汇总 |
| POST /api/v1/rules/sync | 同步已保存的 HTTPS 规则表，完整校验后替换；失败保留上次配置 |
| POST /api/v1/probe | `{"rule_id":"xbox-global","ip":""}`，排队复测；IP 非空则验证后固定 |
| POST /api/v1/probe-batch | `{"platform":""}` 测全部已启用规则，或指定 Xbox／PlayStation／Switch；返回 queued／skipped 规则 ID 数组，逐域名去重、串行测试；暂停返回 409 |
| POST /api/v1/default | `{"rule_id":"xbox-global"}`，撤销当前优选，取消/隔离该规则旧任务，恢复默认解析；之后按计划或手动复测 |
| POST /api/v1/check | `{"domain":"xvcf1.xboxlive.com","type":"A"}`，从 DNS 核心做实际查询 |
| GET /api/v1/backup | 一致性配置与规则导出，不含服务密钥 |
| POST /api/v1/restore | `{"format":2,"config":{...}}`（兼容 format 1），完整校验后恢复 |
| GET /api/v1/events | SSE update 事件，收到后重新读取完整状态 |

状态中的 `app_version` 为程序语义版本（如 `0.1.0`），`app_revision` 为镜像构建注入的源码提交号，未注入时为 `unknown`。原有数值字段 `version`／`applied_version` 仍表示 DNS 策略版本；`feed.version` 表示订阅内容版本，均不用于程序版本展示。

错误返回 JSON 的 `error` 字段。无效格式／配置为 400，来源不允许为 403，任务重复／未启用为 409，DNS 核心不可达的接入检查为 502。未知字段、重复 JSON 值和超过 1 MiB 的输入拒绝处理。

状态中的地址用于主机接入指引，不从请求 Host 或浏览器域名推断，也不表示主机已实际接入。默认 Compose 发布 TCP／UDP 53；管理员更改端口后须自行确认主机支持。

状态接口新增 `service_addresses` 数组，按顺序返回可选局域网 IPv4；`dns_address` 与 `proxy.address` 保持为第一项，无地址时为空字符串。已声明 SERVER_IP／旧 NAS_IP 时优先且仅使用该部署地址，未声明的原生运行枚举本机网卡并去重；回环、公网、链路本地和 IPv6 不返回。容器未声明宿主机地址时返回空数组，不能把桥接网卡 IP 冒充宿主机地址。下拉选择只影响界面展示和复制，不更改绑定或持久配置。

代理随 controller 启动，配置不再包含 `proxy_enabled`。持久化配置与旧备份加载时会移除该字段，包括旧值 false；普通配置 PUT 仍拒绝未知或已退役字段。`status.proxy` 包含只读的 `enabled`（始终为 true）、`address`（同部署地址）、`port`（8082）、`active_transfers`（正在处理的 HTTP 请求及 CONNECT 隧道）、`downloaded_bytes`（从源站读取的累计字节，含协议开销，进程重启归零）。这些流量不计入探测每日汇总。

代理是 controller 的独立 TCP 8082 入口，不受管理 API 的 Host／Origin／自定义头约束，但使用相同的 `allowed_cidrs` 来源限制及公网目标检查。HTTP 只允许绝对 http URL 与端口 80，CONNECT 只允许 443；其他目标按 400 拒绝，非允许来源 403，并发已满 503，无法连接目标 502。停止进程会关闭隧道。接入和转发边界见 [下载代理](download-proxy.md)。

五个记录接口统一接受 `page`（默认 1）、`page_size`（10／20／50／100，默认 10）和 `snapshot`（非负记录 ID，默认 0）。返回 `items`、`page`、`page_size`、`total`、`pages`、`snapshot`、`retention_days`、`max_records`；空列表为第 1／1 页。翻页携带首次响应的 snapshot，排除后续新增记录；过期清理可能减少页数，请求超过末页时返回当前末页。刷新最新时省略 snapshot 或传 0。分页与筛选参数无效返回 400，读取失败返回 500。

查询记录来自 DNS 核心实际观察到的请求，保存到 controller 的数据库，重启后继续保留；`client` 为核心观察到的来源，路由器转发可能合并来源，不推断主机身份。DNS 观察队列有界，负载或管理故障时可能丢弃观察记录，不以日志完整性阻塞解析。

连接记录与 DNS 查询各最多保存 7 天／10,000 条，测速结果 30 天／5,000 条，运行事件 30 天／500 条；每日汇总 90 天／10,000 行。写入立即限制条数，启动及每小时按时间和条数清理；数据库复用已释放页面并执行 WAL 检查点。仅配置导出，不在备份中包含查询记录。数据库 schema 1/2/3 自动升级到 4（新增 connections 表），保留配置、IPv4 策略与历史，删除退役的预算表。升级前备份完整数据卷；旧程序回滚须恢复对应卷。每日汇总从升级后的新测试开始累计，历史明细不反算汇总。

状态包含 `last_tests`、`next_tests`（规则 ID → 时间）及 `feed`（url、attempt、success、version、error）。未同步时间为零值。状态不再提供 budget 字段。配置移除 `probe_bytes`／`daily_bytes`，增加 `feed_url`／`feed_seconds`；普通 PUT 对旧字段返回 400，旧配置备份在恢复路径迁移。恢复默认不自动立即测速，避免刚恢复就被旧任务覆盖；`unfix` 旧接口不再提供。

测速结果的 `bytes` 包含预热读取量，`duration_ms` 包含连接及读取总耗时；排名速度由预热后的 `measured_bytes`／`measured_ms` 计算。

每日汇总按 UTC 日与域名输出 `tests`、`failures`、`health_checks`、`health_failures`、`average_mbps`、`peak_mbps`、`bytes`、`switches`。平均速度只包含成功吞吐样本；首次启用、恢复优选及 IP 替换计入 switches。其分页快照固定行集合，行内当天指标仍可能更新。

内部接口通过容器网络访问，使用 `Authorization: Bearer SERVICE_TOKEN`：DNS 核心提供 PUT /internal/policy、GET /internal/status、POST /internal/check；controller 内部监听提供 POST /internal/observe。生产 Compose 不发布这些端口。

连接记录的 `items` 包含 `id`、`time`、`client`（实际 TCP 来源 IP）、`method`、`domain`、`address`。通过代理来源、目标格式与并发检查后立即记录；HTTP 保存完整 URL，包括路径与查询参数，CONNECT 只保存 `域名:443`，不推断 HTTPS 路径。记录不表示下载成功，不含请求头、Cookie 或正文；写入失败会记录服务错误但不阻断转发。完整 URL 可能含临时签名，数据库仅用于受信任网络，不应直接分享；配置导出不包含连接记录。

前端从记录添加／补充节点仍使用配置 PUT；补充只替换已有规则的 URL，其余字段保留。HTTPS 记录需要人工填写相同域名的完整下载 URL，不自动将根路径作为下载样本。
