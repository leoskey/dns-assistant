import { useEffect, useId, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, active, date, type Status, type Result, type Rule } from "./api";
import { NodeDialog, type NodeDraft } from "./NodeDialog";

export function PlatformList({ status, refresh, message }: { status: Status | null; refresh: () => Promise<void>; message: (s: string) => void }) {
  const id = useId();
  const platforms = ["", "Xbox", "PlayStation", "Switch"];
  const [params, setParams] = useSearchParams();
  const requestedPlatform = params.get("platform") || "";
  const selectedPlatform = platforms.includes(requestedPlatform) ? requestedPlatform : "";
  const rules = status?.rules.filter(rule => !selectedPlatform || rule.platform === selectedPlatform) || [];
  const [history, setHistory] = useState<Result[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    api<{items: Result[]}>("history?filter=all&page_size=100", undefined, "GET", controller.signal)
      .then(page => setHistory(page.items)).catch(() => {});
    return () => controller.abort();
  }, [status]);
  const [busy, setBusy] = useState("");
  const [draft, setDraft] = useState<NodeDraft | null>(null);
  async function probe(rule?: Rule) {
    setBusy(rule?.id || "all");
    try {
      await api(rule ? "probe" : "probe-batch", rule ? { rule_id: rule.id } : { platform: selectedPlatform });
      message("测速任务已排队");
      await refresh();
    } catch (e) { message((e as Error).message); }
    finally { setBusy(""); }
  }
  return <>
    <div className="page-title"><div><h1>节点列表</h1></div>
      <div className="row-actions"><button className="primary" disabled={!!busy || !status?.enabled || !rules.some(r => r.enabled)} onClick={() => void probe()}>一键测速</button><button disabled={!!busy} onClick={() => setDraft({ url: "", platform: selectedPlatform || "Xbox" })}>添加节点</button></div>
    </div>
    {draft && <NodeDialog draft={draft} onClose={() => setDraft(null)} refresh={refresh} message={message} />}
    <div className="list-toolbar platform-tabs">
      <div className="tabs" role="tablist" aria-label="游戏平台">
        {platforms.map((value, index) => (
          <button
            key={value}
            type="button"
            id={`${id}-${value || "all"}`}
            role="tab"
            aria-selected={selectedPlatform === value}
            aria-controls={`${id}-panel`}
            tabIndex={selectedPlatform === value ? 0 : -1}
            onClick={() => setParams(value ? { platform: value } : {}, { replace: true })}
            onKeyDown={e => {
              const next = e.key === "ArrowRight" ? (index + 1) % platforms.length
                : e.key === "ArrowLeft" ? (index + platforms.length - 1) % platforms.length
                : e.key === "Home" ? 0 : e.key === "End" ? platforms.length - 1 : -1;
              if (next < 0) return;
              e.preventDefault();
              const value = platforms[next];
              setParams(value ? { platform: value } : {}, { replace: true });
              (e.currentTarget.parentElement?.children[next] as HTMLButtonElement)?.focus();
            }}
          >
            {value || "全部"}
          </button>
        ))}
      </div>
    </div>
    <div className="table-wrap" id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-${selectedPlatform || "all"}`} tabIndex={0}><table className="platform-table"><thead><tr>{["域名", "当前节点", "下载速度", "健康", "测速时间", "操作"].map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>
      {status && rules.map(rule => {
        const s = status.selections.find(s => s.rule_id === rule.id && s.family === "A");
        const effective = !!s && active(s) && (status.enabled || s.fixed) && rule.enabled && status.dns_connected && status.version === status.applied_version;
        const task = status.tasks.find(t => t.rule_id === rule.id);
        const pending = task?.state === "queued" || task?.state === "running";
        const nodes = Array.from(new Set([...(s?.ip ? [s.ip] : []), ...rule.candidates, ...history.filter(r => r.rule_id === rule.id).map(r => r.ip)]));
        return <tr key={rule.id}><td><strong>{rule.domain}</strong></td><td><span tabIndex={0} title={nodes.length ? `节点列表（当前、配置候选及最近测速记录）\n${nodes.join("\n")}` : "暂无节点，测速后获取"}>{effective ? s.ip : "默认解析"}</span></td><td>{effective && s.speed_mbps > 0 ? `${s.speed_mbps.toFixed(1)} Mbps` : "—"}</td><td><span className="status-pill" title={s?.reason || task?.message}>{!rule.enabled ? "已停用" : pending ? (task.state === "queued" ? "已排队" : "检测中") : task?.state === "error" ? "异常" : effective ? "健康" : "待检测"}</span></td><td>{date(status.last_tests?.[rule.id] || "")}</td><td><button disabled={!!busy || pending || !rule.enabled || !status.enabled} onClick={() => void probe(rule)}>测速</button></td></tr>;
      })}
      {!rules.length && <tr><td colSpan={6} className="empty">{status ? "暂无节点，请添加节点。" : "正在读取节点…"}</td></tr>}
    </tbody></table></div>
  </>;
}
