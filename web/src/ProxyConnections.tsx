import { useEffect, useState } from "react";
import { api, type Status } from "./api";
import { NodeDialog, type NodeDraft } from "./NodeDialog";

type Connection = {
  id: number;
  domain: string;
  client: string;
  ip: string;
  state: string;
  latest_ip: string;
  needs_reconnect: boolean;
};

const states: Record<string, string> = {
  connecting: "正在连接",
  selected: "优选节点",
  unconfigured: "上游解析，未配置优选",
  disabled: "上游解析，规则已停用",
  paused: "上游解析，自动选优已暂停",
  unavailable: "上游解析，暂无有效优选",
  fallback: "优选连接失败，已回退",
};

export function ProxyConnections({
  status,
  refresh,
  message,
}: {
  status: Status;
  refresh: () => Promise<void>;
  message: (value: string) => void;
}) {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState("");
  const [draft, setDraft] = useState<NodeDraft | null>(null);
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    let controller: AbortController;
    async function sample() {
      controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 5000);
      try {
        const next = await api<Connection[]>(
          "proxy/connections",
          undefined,
          "GET",
          controller.signal,
        );
        if (!stopped) {
          setConnections(next);
          setError("");
          setLoaded(true);
        }
      } catch {
        if (!stopped) {
          setError("连接状态暂不可用");
          setConnections([]);
        }
      } finally {
        clearTimeout(timeout);
        if (!stopped) timer = setTimeout(() => void sample(), 3000);
      }
    }
    void sample();
    return () => {
      stopped = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, []);
  async function reconnect(domain: string) {
    if (
      !window.confirm(
        `重新连接 ${domain}？该域名的现有连接会中断，由主机重试或手动继续下载。`,
      )
    )
      return;
    setBusy(domain);
    try {
      const result = await api<{ closed: number }>("proxy/reconnect", {
        domain,
      });
      setConnections((rows) => rows.filter((row) => row.domain !== domain));
      message(
        result.closed
          ? `已断开 ${result.closed} 条连接，等待主机重新连接`
          : "该域名已无活动连接",
      );
    } catch (e) {
      message((e as Error).message);
    } finally {
      setBusy("");
    }
  }
  return (
    <section aria-label="实际代理连接">
      <div className="section-heading">
        <h2>实际代理连接</h2>
      </div>
      {error ? (
        <p role="alert">{error}</p>
      ) : !loaded ? (
        <p role="status">正在读取连接…</p>
      ) : connections.length === 0 ? (
        <p className="muted">暂无活动代理连接</p>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>下载域名 / 来源</th>
                <th>实际节点</th>
                <th>选路状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {connections.map((connection) => {
                const rule = status.rules.find(
                  (r) => r.domain === connection.domain,
                );
                return (
                  <tr key={connection.id}>
                    <td className="connection-address">
                      {connection.domain}
                      <small>{connection.client}</small>
                    </td>
                    <td>{connection.ip || "—"}</td>
                    <td>
                      {states[connection.state] || "未知状态"}
                      {connection.needs_reconnect && (
                        <p>
                          等待重连
                          {connection.latest_ip
                            ? ` → ${connection.latest_ip}`
                            : "以应用当前解析"}
                        </p>
                      )}
                    </td>
                    <td>
                      <div className="row-actions">
                        <button
                          type="button"
                          disabled={!!busy}
                          onClick={() => void reconnect(connection.domain)}
                        >
                          {busy === connection.domain
                            ? "正在断开…"
                            : "重新连接"}
                        </button>
                        {!rule && (
                          <button
                            type="button"
                            onClick={() =>
                              setDraft({
                                domain: connection.domain,
                                url: "",
                                platform: "",
                              })
                            }
                          >
                            补充下载地址
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {draft && (
        <NodeDialog
          draft={draft}
          onClose={() => setDraft(null)}
          refresh={refresh}
          message={message}
        />
      )}
    </section>
  );
}
