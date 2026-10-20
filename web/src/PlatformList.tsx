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
  const exhausted = !!status && status.budget_used >= status.budget_limit;
  async function probe(rule: Rule, ip = "") {
    setWorking(rule.id);
    try {
      await api("probe", { rule_id: rule.id, ip });
      message(ip ? "固定节点验证已排队，通过后才会应用" : "测速已排队");
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
      const result = await api<{ queued: string[]; skipped: string[] }>(
        "probe-batch",
        { platform: selected },
      );
      message(
        `已排队 ${result.queued.length} 条规则${result.skipped.length ? `，${result.skipped.length} 条已有任务或暂时无法排队` : ""}，按预算依次测速`,
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
          <p>按域名查看下载节点。一键提交当前平台的规则，后台依次测速。</p>
        </div>
        <button
          className="primary"
          disabled={
            !status?.enabled ||
            batchBusy ||
            exhausted ||
            !rules.some((r) => r.enabled)
          }
          onClick={() => void batch()}
        >
          <Play aria-hidden="true" />
          {batchBusy ? "提交中…" : "一键测速"}
        </button>
      </div>
      <div className="list-toolbar platform-tabs">
        <div className="tabs" role="tablist" aria-label="游戏平台">
          {tabs.map((platform, index) => (
            <button
              key={platform}
              role="tab"
              aria-selected={selected === platform}
              aria-controls="platform-results"
              tabIndex={selected === platform ? 0 : -1}
              onClick={() => setParams(platform ? { platform } : {})}
              onKeyDown={(e) => {
                const next =
                  e.key === "ArrowRight"
                    ? (index + 1) % tabs.length
                    : e.key === "ArrowLeft"
                      ? (index + tabs.length - 1) % tabs.length
                      : e.key === "Home"
                        ? 0
                        : e.key === "End"
                          ? tabs.length - 1
                          : -1;
                if (next < 0) return;
                e.preventDefault();
                setParams(tabs[next] ? { platform: tabs[next] } : {});
                (
                  e.currentTarget.parentElement?.children[
                    next
                  ] as HTMLButtonElement
                )?.focus();
              }}
            >
              {platform || "全部"}
            </button>
          ))}
        </div>
        <span>
          {rules.length} 条规则 · {rules.filter((r) => r.enabled).length}{" "}
          条已启用
        </span>
      </div>
      {status && !status.enabled && (
        <p className="list-hint">
          自动选优已暂停，请通过左下角按钮恢复后测速。
        </p>
      )}
      {exhausted && (
        <p className="list-hint">
          今日测速预算已用尽，健康检查按预留额度继续；下一预算日恢复吞吐测试。
        </p>
      )}
      <div className="table-wrap" id="platform-results">
        <table className="platform-table">
          <thead>
            <tr>
              <th>平台 / 下载域名</th>
              <th>地址族</th>
              <th>当前节点</th>
              <th>NAS 探测速度</th>
              <th>任务状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rules.map((rule) => {
              const sels = status!.selections.filter(
                (s) => s.rule_id === rule.id,
              );
              const task = status!.tasks.find((t) => t.rule_id === rule.id);
              const pending =
                task?.state === "running" || task?.state === "queued";
              const open = expanded === rule.id;
              return (
                <Fragment key={rule.id}>
                  <tr>
                    <td>
                      <strong>{rule.domain}</strong>
                      <small>
                        {rule.platform}
                        {!rule.enabled ? " · 已停用" : ""}
                      </small>
                    </td>
                    <td>
                      <div className="address-entry">IPv4</div>
                      <div className="address-entry">IPv6</div>
                    </td>
                    <td>
                      {["A", "AAAA"].map((f) => {
                        const selection = sels.find((s) => s.family === f);
                        return (
                          <div className="address-entry" key={f}>
                            <span>
                              {selection && active(selection)
                                ? selection.ip
                                : "上游解析"}
                            </span>
                            <small>
                              {selection?.fixed
                                ? "固定节点"
                                : selection && active(selection)
                                  ? "自动选择"
                                  : "尚无有效节点"}
                            </small>
                          </div>
                        );
                      })}
                    </td>
                    <td>
                      {["A", "AAAA"].map((f) => {
                        const selection = sels.find((s) => s.family === f);
                        return (
                          <div className="address-entry" key={f}>
                            {selection &&
                            active(selection) &&
                            selection.speed_mbps > 0
                              ? `${selection.speed_mbps.toFixed(1)} Mbps`
                              : "待复测"}
                          </div>
                        );
                      })}
                    </td>
                    <td>
                      <span
                        className={
                          "status-pill " +
                          (task?.state === "running" ? "good" : "")
                        }
                      >
                        {!rule.enabled
                          ? "已停用"
                          : task?.state === "running"
                            ? "测速中"
                            : task?.state === "queued"
                              ? "已排队"
                              : task?.state === "error"
                                ? "检查未通过"
                                : "等待下次检查"}
                      </span>
                      <small>{task?.message || "尚无任务记录"}</small>
                    </td>
                    <td>
                      <div className="row-actions">
                        <button
                          disabled={
                            !status!.enabled ||
                            !rule.enabled ||
                            pending ||
                            working === rule.id ||
                            exhausted
                          }
                          onClick={() => void probe(rule)}
                        >
                          测速
                        </button>
                        <button
                          className="quiet"
                          aria-expanded={open}
                          aria-controls={"node-detail-" + rule.id}
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
                      <td colSpan={6}>
                        <div
                          className="node-detail"
                          id={"node-detail-" + rule.id}
                        >
                          <div className="detail-meta">
                            <span>最近任务：{date(task?.time || "")}</span>
                            <span>
                              下次复测：
                              {status!.enabled
                                ? date(status!.next_tests[rule.id])
                                : "恢复选优后继续"}
                            </span>
                          </div>
                          {["A", "AAAA"].map((f) => {
                            const s = sels.find((s) => s.family === f);
                            return (
                              <div className="selection-row" key={f}>
                                <span className="family">
                                  {f === "A" ? "IPv4" : "IPv6"}
                                </span>
                                <div>
                                  <strong>
                                    {s && active(s) ? s.ip : "上游解析"}
                                  </strong>
                                  <p>
                                    {s?.reason || "此地址族尚无有效节点"}
                                    {s?.backup ? `；备用：${s.backup}` : ""}
                                  </p>
                                </div>
                                <span className="speed">
                                  {s && active(s) && s.speed_mbps > 0
                                    ? s.speed_mbps.toFixed(1) + " Mbps"
                                    : "待复测"}
                                </span>
                                <span className="status-pill">
                                  {s?.fixed
                                    ? "固定"
                                    : s && active(s)
                                      ? "自动"
                                      : "常规"}
                                </span>
                              </div>
                            );
                          })}
                          <div className="pin-form">
                            <label htmlFor={"pin-" + rule.id}>
                              固定节点 IP
                            </label>
                            <input
                              id={"pin-" + rule.id}
                              placeholder="输入公网 IP，验证通过后应用"
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
                            <button
                              className="quiet"
                              onClick={async () => {
                                try {
                                  await api("unfix", { rule_id: rule.id });
                                  await refresh();
                                  message("已恢复自动选择");
                                } catch (e) {
                                  message((e as Error).message);
                                }
                              }}
                            >
                              恢复自动
                            </button>
                          </div>
                          <p className="footnote">
                            测试样本：{rule.url}。可在
                            <NavLink to="/settings">设置</NavLink>
                            维护下载域名与样本。
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
                <td colSpan={6} className="empty">
                  {status
                    ? "没有此平台的规则，请在设置中添加。"
                    : "正在读取规则…"}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="footnote">
        IPv4 与 IPv6 分开验证；一键测速会测试规则的可用地址族。结果是 NAS
        采样，HTTPS／SVCB 地址提示仍保留上游结果。
      </p>
    </>
  );
}
