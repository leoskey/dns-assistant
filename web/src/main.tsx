import React, {
  useEffect,
  useState,
  createContext,
  useContext,
  useCallback,
} from "react";
import { createRoot } from "react-dom/client";
import {
  BrowserRouter,
  NavLink,
  Routes,
  Route,
  useLocation,
} from "react-router-dom";
import { ConsoleSetup } from "./ConsoleSetup";
import { PlatformList } from "./PlatformList";
import { RuleListEditor } from "./RuleListEditor";
import {
  Activity,
  ArrowUpRight,
  Check,
  CircleCheck,
  Gamepad2,
  History as HistoryIcon,
  LayoutDashboard,
  ListChecks,
  Network,
  Pause,
  Play,
  Server,
  Settings2,
  X,
  Download,
  Upload,
  RefreshCw,
} from "lucide-react";
import {
  api,
  bytes,
  date,
  active,
  type Config,
  type Status,
  type Selection,
  type Result,
} from "./api";
import "./style.css";
type ContextValue = {
  status: Status | null;
  refresh: () => Promise<void>;
  message: (v: string) => void;
};
const Ctx = createContext<ContextValue>({
  status: null,
  refresh: async () => {},
  message: () => {},
});
const useApp = () => useContext(Ctx);
const platforms = ["Xbox", "PlayStation", "Switch"];
function App() {
  const [status, setStatus] = useState<Status | null>(null),
    [notice, setNotice] = useState(""),
    [connected, setConnected] = useState(false);
  const refresh = useCallback(async () => {
    try {
      setStatus(await api<Status>("status"));
      setConnected(true);
    } catch {
      setConnected(false);
    }
  }, []);
  useEffect(() => {
    void refresh();
    const stream = new EventSource("/api/v1/events");
    stream.addEventListener("update", () => void refresh());
    stream.onerror = () => setConnected(false);
    const timer = setInterval(() => void refresh(), 15000);
    return () => {
      stream.close();
      clearInterval(timer);
    };
  }, [refresh]);
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(""), 6000);
    return () => clearTimeout(t);
  }, [notice]);
  async function toggle() {
    if (!status) return;
    try {
      const c = await api<Config>("config");
      await api("config", { ...c, enabled: !c.enabled }, "PUT");
      await refresh();
      setNotice(
        c.enabled ? "已暂停自动选优，普通解析继续可用" : "已恢复自动选优",
      );
    } catch (e) {
      setNotice((e as Error).message);
    }
  }
  return (
    <Ctx.Provider value={{ status, refresh, message: setNotice }}>
      <BrowserRouter>
        <div className="shell">
          <aside>
            <NavLink className="brand" to="/">
              <Mark />
              <span>
                DNS Assistant<small>家庭游戏网络</small>
              </span>
            </NavLink>
            <nav aria-label="主导航">
              {(
                [
                  ["/", "网络总览", LayoutDashboard],
                  ["/platforms", "平台选优", Gamepad2],
                  ["/connection", "接入检查", Network],
                  ["/history", "测速记录", HistoryIcon],
                  ["/settings", "设置", Settings2],
                ] as const
              ).map(([to, label, Icon]) => (
                <NavLink key={to} end={to === "/"} to={to}>
                  <Icon aria-hidden="true" />
                  {label}
                </NavLink>
              ))}
            </nav>
          </aside>
          <section className="aside-note" aria-label="服务运行状态">
            <div className="aside-status">
              <div className="aside-service" role="status">
                <span
                  className={
                    "dot " + (connected && status?.dns_connected ? "on" : "")
                  }
                />
                {!connected
                  ? "连接中断，正在重试"
                  : status?.dns_connected
                    ? "DNS 与管理服务已连接"
                    : "DNS 核心尚未连接"}
              </div>
              <div className="aside-optimization">
                <span className={"dot " + (status?.enabled ? "on" : "")} />
                {!status
                  ? "正在读取选优状态"
                  : status.enabled
                    ? "自动选优运行中"
                    : "自动选优已暂停"}
              </div>
            </div>
            <button
              className="aside-toggle"
              disabled={!status}
              onClick={() => void toggle()}
            >
              {status?.enabled ? (
                <Pause aria-hidden="true" />
              ) : (
                <Play aria-hidden="true" />
              )}
              {status?.enabled ? "暂停自动选优" : "恢复自动选优"}
            </button>
          </section>
          <main className="workspace">
            {notice && (
              <div className="notice" role="status">
                {notice}
                <button aria-label="关闭提示" onClick={() => setNotice("")}>
                  <X aria-hidden="true" />
                </button>
              </div>
            )}
            <PageRoutes />
          </main>
        </div>
      </BrowserRouter>
    </Ctx.Provider>
  );
}
function PageRoutes() {
  const location = useLocation();
  useEffect(() => {
    document.querySelector(".workspace")?.scrollTo(0, 0);
  }, [location.pathname]);
  return (
    <div className="page-content" key={location.pathname}>
      <Routes>
        <Route path="/" element={<Overview />} />
        <Route path="/platforms" element={<Platforms />} />
        <Route path="/connection" element={<Connection />} />
        <Route path="/history" element={<History />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Overview />} />
      </Routes>
    </div>
  );
}
function Mark() {
  return (
    <svg className="mark" viewBox="0 0 40 40" aria-hidden="true">
      <path
        d="M5 30V10h9c8 0 7 20 15 20h6M5 20h30M35 10v20"
        fill="none"
        stroke="currentColor"
        strokeWidth="3.5"
        strokeLinecap="round"
      />
    </svg>
  );
}
function Title({
  title,
  sub,
  children,
}: {
  title: string;
  sub: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="page-title">
      <div>
        <h1>{title}</h1>
        <p>{sub}</p>
      </div>
      {children}
    </div>
  );
}
function Overview() {
  const { status } = useApp();
  if (!status)
    return (
      <p className="loading" role="status">
        正在读取网络状态…
      </p>
    );
  const chosen = status.selections.filter(active),
    running = status.tasks.filter((t) => t.state === "running");
  return (
    <>
      <Title
        title="网络总览"
        sub="查看解析状态、平台下载节点与后台测速用量。"
      />
      <section className="network-board">
        <div className="board-intro">
          <span className={"status-pill " + (status.enabled ? "good" : "")}>
            {status.enabled && <CircleCheck aria-hidden="true" />}
            {status.enabled ? "自动选优已开启" : "自动选优已暂停"}
          </span>
          <h2>
            {running.length
              ? "正在检测下载节点"
              : chosen.length
                ? "下载节点已就绪"
                : status.tasks.length
                  ? "当前使用上游解析"
                  : "从第一次探测开始"}
          </h2>
          <p>
            {chosen.length
              ? "后台会持续检查节点，表现更好且稳定时自动切换。"
              : status.tasks.length
                ? "还没有通过检查的优选节点。当前使用普通解析，可在平台页查看原因。"
                : "服务已可正常解析。有效下载节点会在自动检测通过后显示。"}
          </p>
        </div>
        <div className="route-diagram" aria-label="主机经本地 DNS 选择 CDN">
          <div>
            <Gamepad2 aria-hidden="true" />
            游戏主机
          </div>
          <i />
          <div className="route-middle">
            <Mark />
            家庭 DNS
          </div>
          <i />
          <div>
            <Server aria-hidden="true" />
            下载节点
          </div>
        </div>
        <div className="board-footer">
          <span>
            <ListChecks aria-hidden="true" />
            {chosen.length} 个有效地址族策略
          </span>
          <span>
            <Activity
              className={running.length ? "task-running" : undefined}
              aria-hidden="true"
            />
            {running.length} 项探测进行中
          </span>
          <span>
            <Check aria-hidden="true" />
            {status.dns_connected && status.version === status.applied_version
              ? "策略已同步"
              : "策略等待同步"}
          </span>
        </div>
      </section>
      <ConsoleSetup
        address={status.dns_address}
        connected={status.dns_connected}
      />
      <div className="section-heading">
        <h2>平台下载节点</h2>
        <NavLink to="/platforms">
          查看全部节点
          <ArrowUpRight aria-hidden="true" />
        </NavLink>
      </div>
      <div className="platform-grid">
        {platforms.map((p) => (
          <PlatformSummary key={p} platform={p} />
        ))}
      </div>
      <section className="budget">
        <div>
          <h3>今日测速用量</h3>
          <p>测试会消耗少量流量；达到预算后延后吞吐复测，优先检查可用性。</p>
        </div>
        <div className="budget-number">
          {bytes(status.budget_used)}{" "}
          <span>/ {bytes(status.budget_limit)}</span>
          <progress
            aria-label="今日测速流量"
            max={status.budget_limit}
            value={status.budget_used}
          />
        </div>
      </section>
      <p className="footnote">
        探测速度来自 NAS
        测试，不代表主机实时下载速度。节点切换在重新解析和建立连接后生效。
      </p>
    </>
  );
}
function PlatformSummary({ platform }: { platform: string }) {
  const { status } = useApp();
  const rules = status!.rules.filter(
    (r) => r.platform === platform && r.enabled,
  );
  const choices = status!.selections.filter(
    (s) => rules.some((r) => r.id === s.rule_id) && active(s),
  );
  const task = status!.tasks.find(
    (t) => rules.some((r) => r.id === t.rule_id) && t.state === "running",
  );
  return (
    <article className={"platform-summary " + platform.toLowerCase()}>
      <div className="platform-name">
        <span className="platform-symbol">
          <Gamepad2 aria-hidden="true" />
        </span>
        <h3>{platform}</h3>
      </div>
      <p className="platform-state">
        {task
          ? "正在测速"
          : choices.length
            ? "已有有效节点"
            : rules.length
              ? "等待有效测速结果"
              : "未启用规则"}
      </p>
      <strong>
        {choices.length && choices[0].speed_mbps > 0
          ? choices[0].speed_mbps.toFixed(1)
          : "—"}
        <small> Mbps</small>
      </strong>
      <p className="ip">{choices[0]?.ip || "暂用上游解析"}</p>
      <NavLink to={"/platforms?platform=" + platform}>
        管理平台节点
        <ArrowUpRight aria-hidden="true" />
      </NavLink>
    </article>
  );
}
function Platforms() {
  return <PlatformList {...useApp()} />;
}
function Connection() {
  const { status, message } = useApp(),
    [domain, setDomain] = useState("xvcf1.xboxlive.com"),
    [type, setType] = useState("A"),
    [check, setCheck] = useState<{
      rcode: string;
      answers: string[];
      duration_ms: number;
    } | null>(null),
    [busy, setBusy] = useState(false);
  return (
    <>
      <Title
        title="接入检查"
        sub="检查下载域名的解析结果，查看最近收到的 DNS 请求。"
      />
      <section className="rule">
        <h2>检查实际 DNS 应答</h2>
        <form
          className="check-form"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              setCheck(await api("check", { domain, type }));
            } catch (e) {
              message((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        >
          <label className="sr-only" htmlFor="domain">
            检查域名
          </label>
          <input
            id="domain"
            required
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
          />
          <label className="sr-only" htmlFor="query-type">
            地址族
          </label>
          <select
            id="query-type"
            value={type}
            onChange={(e) => setType(e.target.value)}
          >
            <option>A</option>
            <option>AAAA</option>
          </select>
          <button className="primary" disabled={busy}>
            {busy ? "检查中…" : "检查解析"}
          </button>
        </form>
        {check && (
          <div className="check-result">
            <strong>
              {check.rcode} · {check.duration_ms} ms
            </strong>
            {check.answers.length ? (
              check.answers.map((v, i) => <pre key={i}>{v}</pre>)
            ) : (
              <p>当前没有此类型的回答。</p>
            )}
          </div>
        )}
      </section>
      <h2 className="section-heading">最近收到的查询</h2>
      <p className="muted">在主机上运行网络测试后查看来源和域名；通过路由器转发时，来源可能只显示路由器。配置方法见首页主机指引。</p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>来源</th>
              <th>域名</th>
              <th>类型</th>
              <th>解析方式</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            {status?.queries.length ? (
              status.queries.slice(0, 30).map((q, i) => (
                <tr key={i}>
                  <td>{q.client}</td>
                  <td>{q.domain}</td>
                  <td>{q.type}</td>
                  <td>{q.optimized ? "优选节点" : "上游解析"}</td>
                  <td>{date(q.time)}</td>
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={5} className="empty">
                  还没有收到查询。请先设置主机 DNS，再重新执行网络测试。
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
function History() {
  const [history, setHistory] = useState<{
      results: Result[];
      events: { time: string; message: string }[];
    } | null>(null),
    [error, setError] = useState(""),
    [filter, setFilter] = useState("all");
  const load = useCallback(async () => {
    try {
      setHistory(await api("history"));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), 10000);
    return () => clearInterval(t);
  }, [load]);
  return (
    <>
      <Title title="测速记录" sub="查看每次探测的证据，以及节点变化的原因。">
        <button onClick={() => void load()}>
          <RefreshCw aria-hidden="true" />
          刷新记录
        </button>
      </Title>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="filter">
        <label htmlFor="result-filter">显示</label>
        <select
          id="result-filter"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        >
          <option value="all">全部探测</option>
          <option value="throughput">吞吐测试</option>
          <option value="health">健康检查</option>
          <option value="failed">失败记录</option>
        </select>
      </div>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>时间与域名</th>
              <th>候选 IP</th>
              <th>类型</th>
              <th>结果</th>
              <th>读取用量</th>
            </tr>
          </thead>
          <tbody>
            {history?.results
              .filter(
                (r) =>
                  filter === "all" ||
                  (filter === "failed" && !r.ok) ||
                  r.kind === filter,
              )
              .map((r) => (
                <tr key={r.id}>
                  <td>
                    <strong>{r.domain}</strong>
                    <small>{date(r.time)}</small>
                  </td>
                  <td>
                    {r.ip}
                    <small>{r.family}</small>
                  </td>
                  <td>{r.kind === "health" ? "健康检查" : "吞吐测试"}</td>
                  <td>
                    {r.ok ? (
                      <span className="success">
                        {r.kind === "throughput"
                          ? r.speed_mbps.toFixed(1) + " Mbps"
                          : "可用"}
                      </span>
                    ) : (
                      <span className="error">{r.error}</span>
                    )}
                  </td>
                  <td>{bytes(r.bytes)}</td>
                </tr>
              ))}
          </tbody>
        </table>
        {history && !history.results.length && (
          <p className="empty">
            尚无测速记录。启用规则后，自动任务会生成第一条结果。
          </p>
        )}
        {!history && !error && <p className="empty">正在读取历史…</p>}
      </div>
      <h2 className="section-heading">运行与切换记录</h2>
      <ul className="event-list">
        {history?.events.map((e, i) => (
          <li key={i}>
            <time>{date(e.time)}</time>
            <span>{e.message}</span>
          </li>
        ))}
      </ul>
    </>
  );
}
function Settings() {
  const { refresh, message } = useApp(),
    [config, setConfig] = useState<Config | null>(null),
    [error, setError] = useState(""),
    [rules, setRules] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    api<Config>("config")
      .then((c) => {
        setConfig(c);
        setRules(JSON.stringify(c.rules, null, 2));
      })
      .catch((e) => setError(e.message));
  }, []);
  function field<K extends keyof Config>(key: K, value: Config[K]) {
    setConfig((c) => (c ? { ...c, [key]: value } : c));
  }
  if (!config)
    return (
      <p className={error ? "error" : "loading"}>{error || "正在读取设置…"}</p>
    );
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const parsed = JSON.parse(rules);
      const updated = { ...config!, rules: parsed };
      await api("config", updated, "PUT");
      setConfig(updated);
      setError("");
      message("设置已保存，后台任务使用新配置");
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Title
        title="设置"
        sub="调整解析、测速用量及平台规则。保存前会检查完整配置。"
      />
      <form onSubmit={save}>
        <section className="settings-section">
          <div>
            <h2>解析与访问</h2>
            <p>普通域名使用这些上游。请勿填写 NAS 自身的 DNS 地址。</p>
          </div>
          <div className="fields">
            <label>
              上游 DNS（每行一个 IP:端口 或 DoH URL）
              <textarea
                rows={3}
                value={config.upstreams.join("\n")}
                onChange={(e) => field("upstreams", e.target.value.split("\n"))}
              />
            </label>
            <label>
              允许访问的局域网网段（每行一个）
              <textarea
                rows={4}
                value={config.allowed_cidrs.join("\n")}
                onChange={(e) =>
                  field("allowed_cidrs", e.target.value.split("\n"))
                }
              />
            </label>
            <label>
              时区
              <input
                value={config.timezone}
                onChange={(e) => field("timezone", e.target.value)}
              />
            </label>
          </div>
        </section>
        <section className="settings-section">
          <div>
            <h2>自动测速</h2>
            <p>后台持续检查。吞吐测试串行执行，预算不足会延后。</p>
          </div>
          <div className="fields two-col">
            {(
              [
                ["daily_bytes", "每日预算（MiB）", 1048576],
                ["probe_bytes", "单节点读取上限（MiB）", 1048576],
                ["probe_seconds", "单节点时间上限（秒）", 1],
                ["interval_seconds", "吞吐复测间隔（秒）", 1],
                ["health_seconds", "健康检查间隔（秒）", 1],
                ["policy_seconds", "健康有效期（秒）", 1],
                ["ttl", "优选应答 TTL（秒）", 1],
                ["cooldown_seconds", "切换冷却（秒）", 1],
                ["improvement", "切换优势（%）", 0.01],
              ] as const
            ).map(([k, label, scale]) => (
              <label key={k}>
                {label}
                <input
                  type="number"
                  step={k === "improvement" ? 1 : "any"}
                  required
                  value={Number((config[k] / scale).toFixed(3))}
                  onChange={(e) => field(k, Number(e.target.value) * scale)}
                />
              </label>
            ))}
          </div>
        </section>
        <section className="settings-section">
          <div>
            <h2>平台下载规则</h2>
            <p>
              每个域名必须有自己的有效测试样本。内置规则会逐域名验证下载样本；只有验证通过的域名才会应用节点。Nintendo
              联网检测与游戏下载应分别配置样本。
            </p>
            <p>
              支持 Xbox 下载、PlayStation 下载及 Nintendo
              下载域名。内容前缀可用于进一步确认样本。
            </p>
          </div>
          <div className="fields">
            <label>
              规则版本
              <input
                required
                value={config.rule_version}
                onChange={(e) => field("rule_version", e.target.value)}
              />
            </label>
            <label>
              规则来源
              <input
                required
                value={config.rule_source}
                onChange={(e) => field("rule_source", e.target.value)}
              />
            </label>
            <RuleListEditor value={rules} onChange={setRules} />
          </div>
        </section>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="save-bar">
          <button className="primary" disabled={busy}>
            {busy ? "保存中…" : "保存设置"}
          </button>
          <span>修改规则会使受影响的旧结果失效。</span>
        </div>
      </form>
      <section className="backup">
        <div>
          <h2>备份与恢复</h2>
          <p>
            导出当前设置和规则，服务密钥不会包含在备份中。恢复会替换当前设置。
          </p>
        </div>
        <a className="button" href="/api/v1/backup" download>
          <Download aria-hidden="true" />
          导出配置
        </a>
        <label className="button file-button">
          <Upload aria-hidden="true" />
          恢复配置
          <input
            type="file"
            accept=".json,application/json"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              if (!file) return;
              if (!window.confirm("恢复将替换当前设置和规则，是否继续？")) {
                e.target.value = "";
                return;
              }
              try {
                if (file.size > 1048576)
                  throw new Error("配置文件不能超过 1 MiB");
                const backup = JSON.parse(await file.text());
                await api("restore", backup);
                const c = await api<Config>("config");
                setConfig(c);
                setRules(JSON.stringify(c.rules, null, 2));
                await refresh();
                message("配置已恢复");
              } catch (err) {
                message((err as Error).message);
              } finally {
                e.target.value = "";
              }
            }}
          />
        </label>
      </section>
    </>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
