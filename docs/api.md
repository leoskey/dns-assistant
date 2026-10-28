# 管理 API


更新：2026-10-20。管理界面无需登录，API 仅允许配置的局域网来源与允许的 Host。除 GET／HEAD 外必须提供 `X-DNS-Request: 1`；浏览器请求还必须同源，不提供跨源 CORS 授权。

| 方法与路径 | 行为 |
| --- | --- |
| GET /healthz | 管理数据库可用检查 |
| GET /api/v1/status | 自动状态、当前策略、任务、下次复测和同步状态，不携带记录列表 |
| GET /api/v1/proxy/traffic | 轻量代理采样：downloaded_bytes 累计下行字节、uptime_seconds 单调运行秒数、started_at 本次管理进程启动时间；沿用管理访问限制 |
| GET /api/v1/config | 当前完整设置 |
| PUT /api/v1/config | 完整校验后事务替换设置，取消旧规则探测 |
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

状态接口的 `dns_address` 来自部署环境 `SERVER_IP`，仅返回有效的非环回单播 IPv4；缺失或无效时返回空字符串。该地址用于主机手动 DNS 指引，不从请求 Host、浏览器域名或容器网卡推断，也不表示主机已实际接入。默认 Compose 发布 TCP／UDP 53；管理员更改端口后须自行确认主机支持。

四个记录接口统一接受 `page`（默认 1）、`page_size`（10／20／50／100，默认 10）和 `snapshot`（非负记录 ID，默认 0）。返回 `items`、`page`、`page_size`、`total`、`pages`、`snapshot`、`retention_days`、`max_records`；空列表为第 1／1 页。翻页携带首次响应的 snapshot，排除后续新增记录；过期清理可能减少页数，请求超过末页时返回当前末页。刷新最新时省略 snapshot 或传 0。分页与筛选参数无效返回 400，读取失败返回 500。

查询记录来自 DNS 核心实际观察到的请求，保存到 controller 的数据库，重启后继续保留；`client` 为核心观察到的来源，路由器转发可能合并来源，不推断主机身份。DNS 观察队列有界，负载或管理故障时可能丢弃观察记录，不以日志完整性阻塞解析。

查询最多保存 7 天／10,000 条，测速结果 30 天／5,000 条，运行事件 30 天／500 条；每日汇总 90 天／10,000 行。写入立即限制条数，启动及每小时按时间和条数清理；数据库复用已释放页面并执行 WAL 检查点。仅配置导出，不在备份中包含查询记录。数据库 schema 1/2 自动升级到 3，保留配置、IPv4 策略与历史，删除退役的预算表。升级前备份完整数据卷；旧程序回滚须恢复对应卷。每日汇总从升级后的新测试开始累计，历史明细不反算汇总。

状态包含 `last_tests`、`next_tests`（规则 ID → 时间）及 `feed`（url、attempt、success、version、error）。未同步时间为零值。状态不再提供 budget 字段。配置移除 `probe_bytes`／`daily_bytes`，增加 `feed_url`／`feed_seconds`；普通 PUT 对旧字段返回 400，旧配置备份在恢复路径迁移。恢复默认不自动立即测速，避免刚恢复就被旧任务覆盖；`unfix` 旧接口不再提供。

测速结果的 `bytes` 包含预热读取量，`duration_ms` 包含连接及读取总耗时；排名速度由预热后的 `measured_bytes`／`measured_ms` 计算。

每日汇总按 UTC 日与域名输出 `tests`、`failures`、`health_checks`、`health_failures`、`average_mbps`、`peak_mbps`、`bytes`、`switches`。平均速度只包含成功吞吐样本；首次启用、恢复优选及 IP 替换计入 switches。其分页快照固定行集合，行内当天指标仍可能更新。

内部接口通过容器网络访问，使用 `Authorization: Bearer SERVICE_TOKEN`：DNS 核心提供 PUT /internal/policy、GET /internal/status、POST /internal/check；controller 内部监听提供 POST /internal/observe。生产 Compose 不发布这些端口。

## 下载代理候选

当前候选状态接口新增 service_addresses 与 proxy（enabled/address/port/active_transfers/downloaded_bytes）。GET /api/v1/connections 使用现有分页参数与管理访问控制，返回代理请求的 time/client/method/domain/address。连接记录不表示下载完成，HTTPS 只包含 CONNECT 目标，完整 HTTP URL 可能含私有参数。
