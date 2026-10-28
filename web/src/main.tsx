import React, {
  useEffect,
  useRef,
  useState,
  createContext,
  useContext,
  useCallback,
} from "react";
import { createRoot } from "react-dom/client";
import { createPortal } from "react-dom";
import { QueryRecords, HistoryRecords, Events } from "./Records";
import {
  BrowserRouter,
  NavLink,
  Routes,
  Route,
  useLocation,
} from "react-router-dom";
import { ConsoleSetup } from "./ConsoleSetup";
import { PlatformList } from "./PlatformList";
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
} from "lucide-react";
import { api, date, active, type Config, type Status } from "./api";
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
                  ["/platforms", "节点列表", Gamepad2],
                  ["/connection", "解析历史", Network],
                  ["/history", "测速记录", HistoryIcon],
                  ["/events", "运行日志", Activity],
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
            <div className="aside-meta">
              <a className="github-link" href="https://github.com/leoskey/dns-assistant" target="_blank" rel="noopener noreferrer" aria-label="在新标签页打开 DNS Assistant GitHub 项目">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M12 .75a11.25 11.25 0 0 0-3.558 21.923c.563.104.768-.244.768-.542 0-.267-.01-.975-.015-1.914-3.13.68-3.791-1.508-3.791-1.508-.512-1.3-1.25-1.647-1.25-1.647-1.023-.7.077-.686.077-.686 1.132.08 1.728 1.162 1.728 1.162 1.006 1.725 2.64 1.227 3.283.938.102-.729.394-1.227.716-1.509-2.499-.284-5.126-1.25-5.126-5.563 0-1.229.439-2.234 1.16-3.021-.116-.285-.503-1.429.11-2.978 0 0 .945-.303 3.094 1.154A10.78 10.78 0 0 1 12 6.18c.956.004 1.919.13 2.818.379 2.148-1.457 3.09-1.154 3.09-1.154.615 1.549.228 2.693.112 2.978.723.787 1.158 1.792 1.158 3.021 0 4.324-2.631 5.276-5.138 5.555.404.35.764 1.042.764 2.1 0 1.516-.014 2.739-.014 3.111 0 .3.203.65.774.54A11.252 11.252 0 0 0 12 .75Z" /></svg>
                GitHub
              </a>
              {status?.app_version && <span data-testid="app-version">v{status.app_version}</span>}
            </div>
          </section>
          <div id="settings-actions" className="settings-actions" />
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
        <Route path="/events" element={<><Title title="运行日志" /><Events /></>} />
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
  children,
}: {
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="page-title">
      <div>
        <h1>{title}</h1>
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
      <Title title="网络总览" />
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
            {chosen.length} 个域名已有优选节点
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
  const choice = choices[0];
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
        {choice && choice.speed_mbps > 0
          ? choice.speed_mbps.toFixed(1)
          : "—"}
        <small> Mbps</small>
      </strong>
      <p className="ip">
        {choice ? <>{choice.domain}<br />{choice.ip}</> : "暂用上游解析"}
      </p>
    </article>
  );
}
function Platforms() {
  return <PlatformList {...useApp()} />;
}
function Connection() {
  const checkDialog = useRef<HTMLDialogElement>(null);
  const { message } = useApp(),
    [domain, setDomain] = useState("xvcf1.xboxlive.com"),
    [check, setCheck] = useState<{
      rcode: string;
      answers: string[];
      duration_ms: number;
    } | null>(null),
    [busy, setBusy] = useState(false);
  return (
    <>
      <div className="page-title">
        <div><h1>解析历史</h1></div>
        <button className="primary" onClick={() => checkDialog.current?.showModal()}>检查实际 DNS 应答</button>
      </div>
      <dialog ref={checkDialog} className="dns-check-dialog" aria-labelledby="dns-check-title" onClick={e => { if (e.target === e.currentTarget) { const rect = e.currentTarget.getBoundingClientRect(); if (e.clientX < rect.left || e.clientX > rect.right || e.clientY < rect.top || e.clientY > rect.bottom) e.currentTarget.close(); } }}>
        <div className="dns-check-heading"><h2 id="dns-check-title">检查实际 DNS 应答</h2><button type="button" aria-label="关闭弹窗" onClick={() => checkDialog.current?.close()}><X aria-hidden="true" /></button></div>
        <form
          className="check-form"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              setCheck(await api("check", { domain, type: "A" }));
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
      </dialog>
      <h2 className="section-heading">最近收到的查询</h2>
      <QueryRecords />
    </>
  );
}
function History() {
  return (
    <>
      <Title title="测速记录" />
      <HistoryRecords />
    </>
  );
}
function Settings() {
  const { refresh, message, status } = useApp(),
    [syncing, setSyncing] = useState(false),
    [config, setConfig] = useState<Config | null>(null),
    [error, setError] = useState(""),
    [rules, setRules] = useState(""),
    [saved, setSaved] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    api<Config>("config")
      .then((c) => {
        setConfig(c);
        setRules(JSON.stringify(c.rules, null, 2));
        setSaved(JSON.stringify(c));
      })
      .catch((e) => setError(e.message));
  }, []);
  function field<K extends keyof Config>(key: K, value: Config[K]) {
    setError("");
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
      setRules(JSON.stringify(updated.rules, null, 2));
      setSaved(JSON.stringify(updated));
      setError("");
      message("设置已保存，后台任务使用新配置");
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  let dirty = true;
  try {
    dirty = saved !== JSON.stringify({ ...config, rules: JSON.parse(rules) });
  } catch {
    /* Invalid JSON is an unsaved edit. */
  }
  return (
    <>
      <Title title="设置" />
      <form id="settings-form" onSubmit={save}>
        <fieldset className="settings-fields" disabled={busy || syncing}>
          <section className="settings-section">
            <div>
              <h2>解析与访问</h2>
            </div>
            <div className="fields">
              <label>
                上游 DNS（每行一个 IP:端口 或 DoH URL）
                <textarea
                  aria-describedby="upstream-warning"
                  rows={3}
                  value={config.upstreams.join("\n")}
                  onChange={(e) =>
                    field("upstreams", e.target.value.split("\n"))
                  }
                />
              </label>
              <p id="upstream-warning" className="muted">
                请勿填写服务主机自身的 DNS 地址。
              </p>
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
            </div>
            <div className="fields">
              {(
                [
                  ["probe_seconds", "单节点测速窗口（秒）", 1],
                  ["interval_seconds", "吞吐复测间隔（秒）", 1],
                  ["health_seconds", "健康检查间隔（秒）", 1],
                ] as const
              ).map(([k, label, scale]) => (
                <label key={k}>
                  {label}
                  <input
                    type="number"
                    step="any"
                    required
                    value={Number((config[k] / scale).toFixed(3))}
                    onChange={(e) => {
                      const n = Number(e.target.value) * scale;
                      field(k, n);
                      if (k === "health_seconds")
                        field(
                          "policy_seconds",
                          Math.max(config.policy_seconds, n * 3 + 600),
                        );
                    }}
                  />
                </label>
              ))}
              <p className="muted">测速按时长读取，不限制流量。</p>
            </div>
          </section>
          <section className="settings-section">
            <div>
              <h2>源站配置表</h2>
            </div>
            <div className="fields">
              <label>
                规则表 URL
                <input
                  type="url"
                  placeholder="https://example.com/download-rules.json"
                  value={config.feed_url}
                  onChange={(e) => field("feed_url", e.target.value)}
                />
              </label>
              <label>
                自动同步间隔（小时）
                <input
                  type="number"
                  min="1"
                  max="168"
                  value={config.feed_seconds / 3600}
                  onChange={(e) =>
                    field("feed_seconds", Number(e.target.value) * 3600)
                  }
                />
              </label>
              <div className="row-actions">
                <button
                  type="button"
                  disabled={syncing || dirty || !config.feed_url}
                  onClick={async () => {
                    setSyncing(true);
                    try {
                      await api("rules/sync", {});
                      const c = await api<Config>("config");
                      setConfig(c);
                      setRules(JSON.stringify(c.rules, null, 2));
                      setSaved(JSON.stringify(c));
                      await refresh();
                      message("规则表已同步");
                    } catch (e) {
                      await refresh();
                      message((e as Error).message);
                    } finally {
                      setSyncing(false);
                    }
                  }}
                >
                  {syncing ? "同步中…" : "立即同步"}
                </button>
                {dirty && <small>先保存设置，再同步规则表。</small>}
              </div>
              <p className="muted">
                最近成功：
                {status?.feed?.success &&
                !status.feed.success.startsWith("0001")
                  ? date(status.feed.success)
                  : "尚未同步"}{" "}
                · 版本：{status?.feed?.version || config.rule_version}
              </p>
              {status?.feed?.error && (
                <p className="error" role="alert">
                  {status.feed.error}；继续使用上次可用规则。
                </p>
              )}
            </div>
          </section>
        </fieldset>
        {createPortal(
          <div className="save-bar">
            <div aria-live="polite">
              <span>
                {error
                  ? "保存失败，请检查输入后重试"
                  : dirty
                    ? "有未保存的修改"
                    : "设置已保存"}
              </span>
              {error && (
                <p className="error" role="alert">
                  {error}
                </p>
              )}
            </div>
            <button
              form="settings-form"
              type="submit"
              className="primary"
              disabled={busy || syncing || !dirty}
            >
              {busy ? "保存中…" : "保存设置"}
            </button>
          </div>,
          document.getElementById("settings-actions")!,
        )}
      </form>
      <section className="backup">
        <div>
          <h2>备份与恢复</h2>
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
            disabled={busy}
            accept=".json,application/json"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              if (!file) return;
              if (!window.confirm("恢复将替换当前设置和规则，是否继续？")) {
                e.target.value = "";
                return;
              }
              setBusy(true);
              try {
                if (file.size > 1048576)
                  throw new Error("配置文件不能超过 1 MiB");
                const backup = JSON.parse(await file.text());
                await api("restore", backup);
                const c = await api<Config>("config");
                setConfig(c);
                setRules(JSON.stringify(c.rules, null, 2));
                setSaved(JSON.stringify(c));
                setError("");
                await refresh();
                message("配置已恢复");
              } catch (err) {
                message((err as Error).message);
              } finally {
                e.target.value = "";
                setBusy(false);
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
