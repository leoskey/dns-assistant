import { useEffect, useState } from "react";
import { api, active, date, type Status, type Config, type Result, type Rule } from "./api";
import { domainFromURL } from "./RuleListEditor";

export function PlatformList({ status, refresh, message }: { status: Status | null; refresh: () => Promise<void>; message: (s: string) => void }) {
  const [history, setHistory] = useState<Result[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    api<{items: Result[]}>("history?filter=all&page_size=100", undefined, "GET", controller.signal)
      .then(page => setHistory(page.items)).catch(() => {});
    return () => controller.abort();
  }, [status]);
  const [busy, setBusy] = useState("");
  const [adding, setAdding] = useState(false);
  const [url, setURL] = useState("");
  const [platform, setPlatform] = useState("Xbox");
  const [error, setError] = useState("");
  async function probe(rule?: Rule) {
    setBusy(rule?.id || "all");
    try {
      await api(rule ? "probe" : "probe-batch", rule ? { rule_id: rule.id } : { platform: "" });
      message("测速任务已排队");
      await refresh();
    } catch (e) { message((e as Error).message); }
    finally { setBusy(""); }
  }
  async function add(e: React.FormEvent) {
    e.preventDefault(); setBusy("add"); setError("");
    try {
      const domain = domainFromURL(url);
      if (!domain || !/^https?:\/\//i.test(url)) throw new Error("请填写完整的 HTTP(S) 下载 URL");
      const c = await api<Config>("config");
      if (c.rules.some(r => r.domain === domain)) throw new Error("该域名已存在");
      const rule: Rule = { id: crypto.randomUUID(), platform, domain, url, enabled: true, candidates: [], min_bytes: 65536 };
      await api("config", { ...c, rules: [...c.rules, rule] }, "PUT");
      await refresh(); setAdding(false); setURL(""); message("节点已添加");
    } catch(e) { setError((e as Error).message); }
    finally { setBusy(""); }
  }
  return <>
    <div className="page-title"><div><h1>节点列表</h1><p>查看各域名的当前节点、下载速度与健康状态。</p></div>
      <div className="row-actions"><button className="primary" disabled={!!busy || !status?.enabled || !status.rules.some(r => r.enabled)} onClick={() => void probe()}>一键测速</button><button onClick={() => { setAdding(true); setError(""); }}>添加节点</button></div>
    </div>
    {adding && <section className="settings-section"><form onSubmit={add} className="fields"><h2>添加节点</h2><label>平台<select value={platform} onChange={e => setPlatform(e.target.value)}>{["Xbox", "PlayStation", "Switch"].map(p => <option key={p}>{p}</option>)}</select></label><label>测试下载 URL<input type="url" required value={url} onChange={e => setURL(e.target.value)} placeholder="https://下载域名/文件路径" /></label><p className="muted">域名自动识别，候选 IPv4 由上游 DNS 获取。请使用支持平台的真实下载文件。</p>{error && <p role="alert" className="error">{error}</p>}<div className="row-actions"><button className="primary" disabled={!!busy}>保存</button><button type="button" disabled={!!busy} onClick={() => setAdding(false)}>取消</button></div></form></section>}
    <div className="table-wrap"><table className="platform-table"><thead><tr>{["域名", "当前节点", "下载速度", "健康", "测速时间", "操作"].map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>
      {status?.rules.map(rule => {
        const s = status.selections.find(s => s.rule_id === rule.id && s.family === "A");
        const effective = !!s && active(s) && (status.enabled || s.fixed) && rule.enabled && status.dns_connected && status.version === status.applied_version;
        const task = status.tasks.find(t => t.rule_id === rule.id);
        const pending = task?.state === "queued" || task?.state === "running";
        const nodes = Array.from(new Set([...(s?.ip ? [s.ip] : []), ...rule.candidates, ...history.filter(r => r.rule_id === rule.id).map(r => r.ip)]));
        return <tr key={rule.id}><td><strong>{rule.domain}</strong></td><td><span tabIndex={0} title={nodes.length ? `节点列表（当前、配置候选及最近测速记录）\n${nodes.join("\n")}` : "暂无节点，测速后获取"}>{effective ? s.ip : "默认解析"}</span></td><td>{effective && s.speed_mbps > 0 ? `${s.speed_mbps.toFixed(1)} Mbps` : "—"}</td><td><span className="status-pill" title={s?.reason || task?.message}>{!rule.enabled ? "已停用" : pending ? (task.state === "queued" ? "已排队" : "检测中") : task?.state === "error" ? "异常" : effective ? "健康" : "待检测"}</span></td><td>{date(status.last_tests?.[rule.id] || "")}</td><td><button disabled={!!busy || pending || !rule.enabled || !status.enabled} onClick={() => void probe(rule)}>测速</button></td></tr>;
      })}
      {!status?.rules.length && <tr><td colSpan={6} className="empty">{status ? "暂无节点，请添加节点。" : "正在读取节点…"}</td></tr>}
    </tbody></table></div><p className="footnote">速度来自 NAS 测试，不代表主机实时下载速度。</p>
  </>;
}
