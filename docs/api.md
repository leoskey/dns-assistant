# 管理 API

更新：2026-10-20。管理界面无需登录，API 仅允许配置的局域网来源与允许的 Host。除 GET／HEAD 外必须提供 `X-DNS-Request: 1`；浏览器请求还必须同源，不提供跨源 CORS 授权。

| 方法与路径 | 行为 |
| --- | --- |
| GET /healthz | 管理数据库可用检查 |
| GET /api/v1/status | 自动状态、当前策略、预算、任务、下次复测、查询和同步状态 |
| GET /api/v1/config | 当前完整设置 |
| PUT /api/v1/config | 完整校验后事务替换设置，取消旧规则探测 |
| GET /api/v1/history | 最近 300 条结果与 100 条事件 |
| POST /api/v1/probe | `{"rule_id":"xbox-global","ip":""}`，排队复测；IP 非空则验证后固定 |
| POST /api/v1/probe-batch | `{"platform":""}` 测全部已启用规则，或指定 Xbox／PlayStation／Switch；返回 queued／skipped 规则 ID 数组，逐域名去重，沿用串行 worker 与预算；暂停或预算耗尽返回 409 |
| POST /api/v1/unfix | `{"rule_id":"xbox-global"}`，解除固定并恢复自动选择 |
| POST /api/v1/check | `{"domain":"xvcf1.xboxlive.com","type":"A"}`，从 DNS 核心做实际查询 |
| GET /api/v1/backup | 一致性配置与规则导出，不含服务密钥 |
| POST /api/v1/restore | `{"format":1,"config":{...}}`，完整校验后恢复 |
| GET /api/v1/events | SSE update 事件，收到后重新读取完整状态 |

错误返回 JSON 的 `error` 字段。无效格式／配置为 400，来源不允许为 403，任务重复／未启用为 409，DNS 核心不可达的接入检查为 502。未知字段、重复 JSON 值和超过 1 MiB 的输入拒绝处理。

状态接口的 `dns_address` 来自部署环境 `NAS_IP`，仅返回有效的非环回单播 IP；缺失或无效时返回空字符串。该地址用于主机手动 DNS 指引，不从请求 Host、浏览器域名或容器网卡推断，也不表示主机已实际接入。默认 Compose 发布 TCP／UDP 53；管理员更改端口后须自行确认主机支持。

最近查询来自本次运行期最近 100 条实际请求；`client` 为 DNS 核心观察到的来源，路由器转发可能合并来源，不推断主机身份。

内部接口通过容器网络访问，使用 `Authorization: Bearer SERVICE_TOKEN`：DNS 核心提供 PUT /internal/policy、GET /internal/status、POST /internal/check；controller 内部监听提供 POST /internal/observe。生产 Compose 不发布这些端口。
