import { Fragment, useState } from "react";
import { NavLink, useSearchParams } from "react-router-dom";
import { ChevronDown, ChevronUp, Play } from "lucide-react";
import { api, active, date, type Status, type Rule } from "./api";

export function PlatformList({
  status,
  refresh,
  message,
}: {
  status: Status | null;
  refresh: () => Promise<void>;
  message: (s: string) => void;
}) {
  const [params, setParams] = useSearchParams();
  const selected = ["Xbox", "PlayStation", "Switch"].includes(
    params.get("platform") || "",
  )
    ? params.get("platform")!
    : "";
  const tabs = ["", "Xbox", "PlayStation", "Switch"];
  const [expanded, setExpanded] = useState("");
  const [pin, setPin] = useState<Record<string, string>>({});
  const [working, setWorking] = useState("");
  const [batchBusy, setBatchBusy] = useState(false);
  const rules =
    status?.rules.filter((r) => !selected || r.platform === selected) || [];
  async function probe(rule: Rule, ip = "") {
    setWorking(rule.id);
    try {
      await api("probe", { rule_id: rule.id, ip });
      message(ip ? "固定节点验证已排队" : "已排队复测，完成前保持当前解析");
      await refresh();
    } catch (e) {
      message((e as Error).message);
    } finally {
      setWorking("");
    }
  }
  async function restore(rule: Rule) {
    setWorking(rule.id);
    try {
      await api("default", { rule_id: rule.id });
      message("已恢复默认解析，可立即复测或等待定时测试");
      await refresh();
    } catch (e) {
      message((e as Error).message);
    } finally {
      setWorking("");
    }
  }
  async function batch() {
    setBatchBusy(true);
    try {
      const r = await api<{ queued: string[]; skipped: string[] }>(
        "probe-batch",
        { platform: selected },
      );
      message(
        `已排队 ${r.queued.length} 条，跳过 ${r.skipped.length} 条重复或不可用任务`,
      );
      await refresh();
    } catch (e) {
      message((e as Error).message);
    } finally {
      setBatchBusy(false);
    }
  }
  return (
    <>
      <div className="page-title">
        <div>
          <h1>平台选优</h1>
          <p>按持续下载速度选择 IPv4。复测完成后更新，故障时先恢复默认解析。</p>
        </div>
        <button
          className="primary"
          disabled={
            !status?.enabled || batchBusy || !rules.some((r) => r.enabled)
          }
          onClick={() => void batch()}
        >
          <Play aria-hidden="true" />
          {batchBusy ? "提交中…" : "一键复测"}
        </button>
      </div>
      <div className="list-toolbar platform-tabs">
        <div className="tabs" role="tablist" aria-label="游戏平台">
          {tabs.map((p, i) => (
            <button
              key={p}
              id={`platform-tab-${i}`}
              role="tab"
              aria-selected={selected === p}
              aria-controls="platform-results"
              tabIndex={selected === p ? 0 : -1}
              onKeyDown={(e) => {
                const next =
                  e.key === "ArrowRight"
                    ? (i + 1) % tabs.length
                    : e.key === "ArrowLeft"
                      ? (i + tabs.length - 1) % tabs.length
                      : e.key === "Home"
                        ? 0
                        : e.key === "End"
                          ? tabs.length - 1
                          : -1;
                if (next < 0) return;
                e.preventDefault();
                setParams(tabs[next] ? { platform: tabs[next] } : {});
                (
                  e.currentTarget.parentElement?.children[next] as
                    HTMLButtonElement | undefined
                )?.focus();
              }}
              onClick={() => setParams(p ? { platform: p } : {})}
            >
              {p || "全部"}
            </button>
          ))}
        </div>
      </div>
      <div
        className="table-wrap"
        id="platform-results"
        role="tabpanel"
        aria-label={selected || "全部平台"}
      >
        <table className="platform-table">
          <thead>
            <tr>
              <th>平台与域名</th>
              <th>当前解析</th>
              <th>下载速度</th>
              <th>状态与计划</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rules.map((rule) => {
              const s = status!.selections.find(
                (s) => s.rule_id === rule.id && s.family === "A",
              );
              const proposed =
                !!s &&
                active(s) &&
                (status!.enabled || s.fixed) &&
                rule.enabled;
              const waitingSync =
                !status!.dns_connected ||
                status!.version !== status!.applied_version;
              const effective = proposed && !waitingSync;
              const task = status!.tasks.find((t) => t.rule_id === rule.id);
              const pending =
                task?.state === "queued" || task?.state === "running";
              const open = expanded === rule.id;
              return (
                <Fragment key={rule.id}>
                  <tr>
                    <td>
                      <strong>{rule.platform}</strong>
                      <small>{rule.domain}</small>
                    </td>
                    <td>
                      <strong>
                        {effective
                          ? s.ip
                          : waitingSync
                            ? "等待同步"
                            : "默认解析"}
                      </strong>
                      <small>
                        {effective
                          ? s.fixed
                            ? "固定节点"
                            : "优选节点"
                          : "由上游返回可用 IPv4"}
                      </small>
                    </td>
                    <td>
                      {effective && s.speed_mbps > 0
                        ? `${s.speed_mbps.toFixed(1)} Mbps`
                        : "—"}
                    </td>
                    <td>
                      <span
                        className={`status-pill ${task?.state === "running" ? "good" : ""}`}
                      >
                        {!rule.enabled
                          ? "已停用"
                          : task?.state === "running"
                            ? task.kind === "health"
                              ? "健康检查中"
                              : "复测中"
                            : task?.state === "queued"
                              ? "已排队"
                              : task?.state === "error"
                                ? "测试未通过"
                                : waitingSync
                                  ? "策略同步中"
                                  : effective
                                    ? "优选使用中"
                                    : "默认使用中"}
                      </span>
                      <small>
                        {s?.reason ||
                          task?.message ||
                          "首次测试完成前使用默认解析"}
                      </small>
                      <small>
                        最近复测：{date(status!.last_tests?.[rule.id] || "")}
                      </small>
                      <small>
                        下次复测：
                        {status!.enabled && rule.enabled
                          ? date(status!.next_tests[rule.id])
                          : "已暂停"}
                      </small>
                    </td>
                    <td>
                      <div className="row-actions">
                        <button
                          disabled={
                            !status!.enabled ||
                            !rule.enabled ||
                            pending ||
                            working === rule.id
                          }
                          onClick={() => void probe(rule)}
                        >
                          立即复测
                        </button>
                        <button
                          disabled={
                            (!proposed && !pending) || working === rule.id
                          }
                          onClick={() => void restore(rule)}
                        >
                          恢复默认
                        </button>
                        <button
                          className="quiet"
                          aria-expanded={open}
                          aria-controls={`node-detail-${rule.id}`}
                          onClick={() => setExpanded(open ? "" : rule.id)}
                        >
                          {open ? (
                            <ChevronUp aria-hidden="true" />
                          ) : (
                            <ChevronDown aria-hidden="true" />
                          )}
                          详情
                        </button>
                      </div>
                    </td>
                  </tr>
                  {open && (
                    <tr className="expanded-row">
                      <td colSpan={5}>
                        <div
                          className="node-detail"
                          id={`node-detail-${rule.id}`}
                        >
                          <p>
                            最近验证：
                            {s?.checked_at
                              ? date(s.checked_at)
                              : "尚无验证记录"}
                            。{task?.message || "等待下一次计划任务"}
                          </p>
                          <p>
                            测试样本：{rule.url}。候选来自上游 A
                            应答及配置表，不保证包含源站的全部 IP。
                          </p>
                          <details>
                            <summary>高级操作：固定节点</summary>
                            <div className="pin-form">
                              <label htmlFor={`pin-${rule.id}`}>
                                公网 IPv4
                              </label>
                              <input
                                id={`pin-${rule.id}`}
                                placeholder="验证通过后才会固定"
                                value={pin[rule.id] || ""}
                                onChange={(e) =>
                                  setPin({ ...pin, [rule.id]: e.target.value })
                                }
                              />
                              <button
                                disabled={
                                  !pin[rule.id] ||
                                  !rule.enabled ||
                                  pending ||
                                  working === rule.id
                                }
                                onClick={() => void probe(rule, pin[rule.id])}
                              >
                                验证并固定
                              </button>
                            </div>
                          </details>
                          <p className="footnote">
                            修改下载样本和候选地址请前往{" "}
                            <NavLink to="/settings">设置</NavLink>。
                          </p>
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
            {!rules.length && (
              <tr>
                <td colSpan={5} className="empty">
                  {status
                    ? "没有此平台规则，请在设置中添加或同步规则表。"
                    : "正在读取规则…"}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="footnote">
        速度来自服务端
        测试，不代表主机实时下载速度。节点变化在客户端重新解析并建立连接后生效。
      </p>
    </>
  );
}
