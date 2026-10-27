import { useEffect, useId, useRef, useState } from "react";
import { Check, ChevronDown, X } from "lucide-react";
import * as Select from "@radix-ui/react-select";
import { useSearchParams } from "react-router-dom";
import { api, active, date, type Status, type Config, type Result, type Rule } from "./api";
import { domainFromURL } from "./RuleListEditor";

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
  const addDialog = useRef<HTMLDialogElement>(null);
  const urlInput = useRef<HTMLInputElement>(null);
  const [url, setURL] = useState("");
  const [platform, setPlatform] = useState("Xbox");
  const [error, setError] = useState("");
  async function probe(rule?: Rule) {
    setBusy(rule?.id || "all");
    try {
      await api(rule ? "probe" : "probe-batch", rule ? { rule_id: rule.id } : { platform: selectedPlatform });
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
      await refresh(); addDialog.current?.close(); setURL(""); message("节点已添加");
    } catch(e) { setError((e as Error).message); }
    finally { setBusy(""); }
  }
  return <>
    <div className="page-title"><div><h1>节点列表</h1></div>
      <div className="row-actions"><button className="primary" disabled={!!busy || !status?.enabled || !rules.some(r => r.enabled)} onClick={() => void probe()}>一键测速</button><button disabled={!!busy} onClick={() => { setError(""); if (selectedPlatform) setPlatform(selectedPlatform); addDialog.current?.showModal(); urlInput.current?.focus(); }}>添加节点</button></div>
    </div>
    <dialog
      ref={addDialog}
      className="dns-check-dialog"
      aria-labelledby="add-node-title"
      onCancel={e => { if (busy) e.preventDefault(); }}
      onClick={e => {
        if (!busy && e.target === e.currentTarget) {
          const rect = e.currentTarget.getBoundingClientRect();
          if (e.clientX < rect.left || e.clientX > rect.right || e.clientY < rect.top || e.clientY > rect.bottom) e.currentTarget.close();
        }
      }}
    >
      <div className="dns-check-heading">
        <h2 id="add-node-title">添加节点</h2>
        <button type="button" aria-label="关闭弹窗" disabled={!!busy} onClick={() => addDialog.current?.close()}><X aria-hidden="true" /></button>
      </div>
      <form onSubmit={add}>
        <fieldset className="fields settings-fields" disabled={!!busy}>
          <div className="select-field">
            <label htmlFor={`${id}-add-platform`}>平台</label>
            <Select.Root value={platform} onValueChange={setPlatform} disabled={!!busy}>
              <Select.Trigger id={`${id}-add-platform`} className="select-trigger">
                <Select.Value />
                <Select.Icon><ChevronDown aria-hidden="true" /></Select.Icon>
              </Select.Trigger>
              {/* Keep the menu inside the native dialog's top layer. */}
              <Select.Content className="select-content" position="popper" align="start" sideOffset={4} collisionPadding={8}>
                <Select.Viewport>
                  {platforms.slice(1).map(p => (
                    <Select.Item className="select-item" key={p} value={p}>
                      <Select.ItemText>{p}</Select.ItemText>
                      <Select.ItemIndicator><Check size={15} aria-hidden="true" /></Select.ItemIndicator>
                    </Select.Item>
                  ))}
                </Select.Viewport>
              </Select.Content>
            </Select.Root>
          </div>
          <label>测试下载 URL<input ref={urlInput} type="url" required value={url} onChange={e => setURL(e.target.value)} placeholder="https://下载域名/文件路径" /></label>
          {error && <p role="alert" className="error">{error}</p>}
          <div className="row-actions">
            <button className="primary">{busy === "add" ? "保存中…" : "保存"}</button>
            <button type="button" onClick={() => addDialog.current?.close()}>取消</button>
          </div>
        </fieldset>
      </form>
    </dialog>
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
