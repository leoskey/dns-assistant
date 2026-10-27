# DNS 引擎比较与建议

调研日期：2026-10-20。状态：上游资料对比。当前实现已采用 dnsproxy，软件与容器证据见 [验证记录](../validation.md)，NAS 和真实主机仍分别验收。本文件仅使用项目官方文档、上游源码与 Go 官方包索引中的上游文档；不以宣传性能数字作为本产品指标。

## 当前有效需求

中国大陆家宽，支持 Docker 的设备使用 Compose 部署，PS、Xbox、Switch 手动使用服务 DNS；NAS 为可选部署设备。服务启动后以及运行期间自动筛选候选节点、测量可用性和下载吞吐、选择并发布有效 IP；DNS 请求不等待吞吐测试。无合格结果时正常转发上游；后台异常不能让解析阻塞。当前产品仅优选 IPv4，测速按时间窗口执行。此处“最优”指本轮验证过的候选中当前下载速度最佳，不保证全网全局最优。

## 推荐结论

产品需求不绑定 AdGuard 供应方，也不要求部署 AdGuard Home。这里评估的是可替换的 DNS 代理库，实际实现依据公开接口与协议测试采用 dnsproxy；供应方仍是可替换的技术依赖。

当前采用 **Go 嵌入 AdguardTeam/dnsproxy + 自研后台吞吐选优器 + 原子策略快照**。它已经提供代理、缓存、加密上游及回退设施，自有代码集中在域名兼容验证、测量调度与策略应用。采用此方案仍然承担其底层 miekg/dns v1 的过渡风险，需要隔离引擎适配层并持续观察上游迁移。[dnsproxy 功能](https://github.com/AdguardTeam/dnsproxy#readme)、[底层依赖](https://github.com/AdguardTeam/dnsproxy/blob/master/proxy/proxy.go)

保留 **CoreDNS + 自定义策略插件**为备选。若 dnsproxy 的自定义响应路径不能以公开 API 完整处理 EDNS、UDP 截断和缓存边界，重新比较底层协议库与 CoreDNS 插件的集成成本；本项目的具体实现与测试结果分别见技术选型及验证记录。以上优先级是本项目基于实现边界作出的判断。

## 候选比较

| 方案 | 协议与上游 | 缓存与扩展边界 | 对本项目的判断 |
|---|---|---|---|
| github.com/miekg/dns 旧版 | UDP/TCP、IPv4/IPv6、DoT；DoH 要另加实现 | 协议库，服务端/客户端 API 齐全，但产品需要的缓存、上游选择、回退和配置生命周期仍需组装 | 不把它当成完整 DNS 代理；不作为新项目直接实现整套代理的首选 [上游 README](https://github.com/miekg/dns#readme) |
| codeberg.org/miekg/dns 新一代（上游称 DNS version 2） | 上游文档列出 UDP/TCP、IPv4/IPv6、DoT 和 dnshttp 中的 DoH | 仍是底层库；上游明确年轻、允许不兼容变更，Go 包索引当前仍显示未稳定状态 | 作为迁移研究项；“第二代”不能写成已发布稳定语义版本 v2，也不把上游速度宣称当 NAS 结果 [上游文档的 Go 官方索引](https://pkg.go.dev/codeberg.org/miekg/dns) |
| AdguardTeam/dnsproxy | UDP/TCP、IPv4/IPv6；支持 DoH/DoT/DoQ/DNSCrypt 等上游 | 有缓存、超时与回退、按域名上游；Config.RequestHandler 可替代默认处理器 | 主候选；业务策略通过自有快照更新，监听端口和上游配置变更另走受控重启，不假定 CLI YAML 自动热更新 [README](https://github.com/AdguardTeam/dnsproxy#readme)、[Config](https://github.com/AdguardTeam/dnsproxy/blob/master/proxy/config.go) |
| CoreDNS 插件 | UDP/TCP 与 IPv4/IPv6；当前 forward 文档含 DoT、DoH | cache、reload、hosts 等已有插件；业务逻辑可独立插件承载 | 备选；插件编译和发布链更重，频繁策略更新应走插件内快照，不反复重载整个 Corefile [forward](https://coredns.io/plugins/forward/)、[插件模型](https://coredns.io/2017/07/23/corefile-explained/) |
| SmartDNS | UDP/TCP、双栈、DoT/DoH 等，配置含缓存持久化与规则 | 内置探测选 IP；域名规则可指定地址、分组及探测方式；动态规则提交和缓存刷新需要单独验证 | 适合作为家庭 DNS 对照基线；其内置速度选择不是游戏下载吞吐排名，仍需额外选优器 [官方示例配置](https://github.com/pymumu/smartdns/blob/master/etc/smartdns/smartdns.conf) |

## 维护状态与许可证声明

| 项目 | 当前已核验的上游状态 | 上游许可证声明 |
|---|---|---|
| miekg/dns 旧版 | README 公告只接受特定修复，未来会归档，指向 Codeberg 新一代 | BSD-3-Clause [README 与 LICENSE](https://github.com/miekg/dns) |
| Codeberg 新一代 | 上游文档仍强调接口可能发生不兼容变化；直接访问 Codeberg 被抓取规则阻止，本次通过 Go 官方索引阅读上游文档，落地时应重新核验锁定版本 | BSD-3-Clause（Go 官方索引展示的上游包声明）[包文档](https://pkg.go.dev/codeberg.org/miekg/dns) |
| dnsproxy | 官方发布页有近期发布记录；包 API 仍会变化，例如 fastip 已转内部包，不应依赖 internal 实现 | Apache-2.0 [发布页](https://github.com/AdguardTeam/dnsproxy/releases)、[LICENSE](https://github.com/AdguardTeam/dnsproxy/blob/master/LICENSE) |
| CoreDNS | 官方发布页有维护和安全修复记录 | Apache-2.0 [发布页](https://github.com/coredns/coredns/releases)、[LICENSE](https://github.com/coredns/coredns/blob/master/LICENSE) |
| SmartDNS | 官方发布页有持续修复发布记录 | 仓库标识 GPL-3.0；只记录该声明，不推导组合、分发或插件的许可结论 [发布页](https://github.com/pymumu/smartdns/releases)、[仓库声明](https://github.com/pymumu/smartdns)、[LICENSE](https://github.com/pymumu/smartdns/blob/master/LICENSE) |

这些记录描述调研时看到的上游状态，不保证未来维护。未选择或锁定未经构建验证的精确版本。

## 内置“最快地址”不能替代吞吐测试

- dnsproxy 的 fastest_addr 对返回 IP 做 TCP 连接竞争，源码默认端口为 80/443，取最先成功建立连接的地址；它不请求平台下载内容。因此本项目不启用此模式作为游戏下载选优依据。[fastip 源码](https://github.com/AdguardTeam/dnsproxy/blob/master/internal/fastip/fastest.go)
- SmartDNS 的 speed-check-mode 配置为 ping、tcp:port、tcp-syn:port 或 none。它能够筛出响应较快的地址，但这类探测没有测量同域名内容在连续下载中的吞吐。不要把“响应最快”显示为“下载速度最快”。[官方配置](https://github.com/pymumu/smartdns/blob/master/etc/smartdns/smartdns.conf)
- CoreDNS forward 的健康检查验证 DNS 上游可响应，其负载分配和健康检查不等于返回 CDN 地址的内容兼容或下载吞吐验证。[forward](https://coredns.io/plugins/forward/)

本项目的下载探测必须独立工作：保留原域名 Host/SNI 与证书验证，将连接定向到候选 IP；使用受限内容读取验证响应与吞吐；按平台、域名保存 IPv4 结果。HTTP Range、重定向、测试对象有效期、时间窗口及取消均属于本项目自研逻辑及持续回归范围。

## dnsproxy 接入与缓存边界

已查到的上游调用链如下：

1. server.go 的 handleDNSRequest 先 validateRequest，再调用配置的 requestHandler.ServeDNS，最后 respond。
2. requesthandler.go 中 DefaultHandler 只调用 Proxy.Resolve。
3. Proxy.Resolve 内执行 ECS 与响应标志初始化、请求合并、普通缓存查找，再请求上游、缓存和整理响应。

证据：[server.go](https://github.com/AdguardTeam/dnsproxy/blob/master/proxy/server.go)、[requesthandler.go](https://github.com/AdguardTeam/dnsproxy/blob/master/proxy/requesthandler.go)、[Resolve 实现](https://github.com/AdguardTeam/dnsproxy/blob/master/proxy/proxy.go)。

据此建议自定义处理器先检查经过验证且未过期的原子策略；命中策略则构建当前响应，未命中委托 DefaultHandler。这样业务策略先于普通上游缓存读取，历史优选 IP 不写入普通解析缓存；后台策略切换无需重启监听器。需针对上游普通解析缓存与业务策略快照各自设计有效期。

**自定义应答必须保留的边界：**直接填充 DNSContext.Res 会绕过 Resolve 内部的一些标志处理、scrub 和 filterMsg。不能假定自定义 Handler 已获得完整默认响应处理。协议测试必须持续核验 UDP 长度/TC、EDNS、DO/CD/AD、RA/AA、CNAME 链和无匹配地址族行为，确定是否通过公开 API 或受控适配完整保留这些行为；无法做到就改用 CoreDNS 插件路径。普通缓存提供 ClearCache，但不应每次业务策略变化都清空所有域名的缓存。[缓存 API](https://github.com/AdguardTeam/dnsproxy/blob/master/proxy/proxycache.go)

## CoreDNS 备选的具体边界

- 当前官方 forward 文档已明确支持 DoH；其实现文档要求默认 /dns-query 路径，不能假定支持任意自定义路径。开发时用锁定发布版本验证，而非仅以网站文档判断旧镜像能力。[forward](https://coredns.io/plugins/forward/)
- hosts 可定期重新读取文件并支持 A/AAAA/PTR；它适合简单原型，但不足以自然表达完整 CNAME 关系、策略有效期与地址族回退状态。[hosts](https://coredns.io/plugins/hosts/)
- cache 的 serve_stale 等功能需要按业务语义配置；被本地重写的节点失效后不能继续当作普通上游记录无限延长有效期。[cache](https://coredns.io/plugins/cache/)
- reload 为 Corefile 变更提供重载，但官方记载更换监听端口时存在失败边界；频繁选 IP 应在固定监听器中的策略模块更新。[reload](https://coredns.io/plugins/reload/)

## POC 验收门禁

| 验证项 | 通过条件 |
|---|---|
| 可构建与可发布 | 锁定依赖后，Linux amd64/arm64 构建与容器启动；记录许可证声明及依赖版本 |
| DNS 协议 | UDP/TCP、IPv4/IPv6、CNAME、TTL、负缓存、EDNS 大响应与 TCP 回退保持合理行为；合成响应不冒充 DNSSEC 验证成功 |
| 策略与缓存 | 旧上游缓存存在时能立即应用新策略；策略失效后恢复普通解析；无策略时不等待测速；切换并发无数据竞态 |
| 上游 | UDP/TCP/DoH 的启动解析、证书校验、超时与回退通过；禁止 DNS 自指环路 |
| 吞吐探测 | 原域名 Host/SNI 不变；证书、响应和内容受检查；Range 被忽略时仍能强制停止读取；测量窗口、预热与取消生效 |
| NAS 网络 | 主机查询真实到达；NAS 与主机使用相同外网出口；53 端口冲突、宿主机防火墙、IPv4 接入和重启恢复通过 |
| 故障隔离 | 管理服务或测速任务异常时，DNS 已发布策略和普通解析继续服务；策略到期后有可预测行为 |
| 主机实测 | 三平台真实内容多轮对比，分开报告服务端探测与主机实际下载收益 |

本文件记录上游事实和实现约束。当前代码、构建、容器与真实主机的证据以 [验证记录](../validation.md) 为准，不从上游能力推断本产品已通过实机验收。
