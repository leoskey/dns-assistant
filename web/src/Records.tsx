import { useEffect, useId, useState } from "react";
import { RefreshCw } from "lucide-react";
import {
  api,
  bytes,
  date,
  type Result,
  type Query,
  type ConnectionRecord,
  type Status,
} from "./api";
import { NodeDialog, type NodeDraft } from "./NodeDialog";

export type RecordPage<T> = {
  items: T[];
  page: number;
  page_size: number;
  total: number;
  pages: number;
  snapshot: number;
  retention_days: number;
  max_records: number;
};
type EventRecord = { id: number; time: string; message: string };

function useRecords<T>(path: string) {
  const [request, setRequest] = useState({
    page: 1,
    size: 10,
    snapshot: 0,
    revision: 0,
  });
  const [data, setData] = useState<RecordPage<T> | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    let loading = false;
    async function load() {
      if (loading) return;
      loading = true;
      setBusy(true);
      try {
        const query = new URLSearchParams({
          page: String(request.page),
          page_size: String(request.size),
          snapshot: String(request.snapshot),
        });
        const page = await api<RecordPage<T>>(
          `${path}${path.includes("?") ? "&" : "?"}${query}`,
          undefined,
          "GET",
          controller.signal,
        );
        if (active) {
          setData(page);
          setError("");
        }
      } catch (err) {
        if (active) setError((err as Error).message);
      } finally {
        loading = false;
        if (active) setBusy(false);
      }
    }
    void load();
    // Only the live first page polls. Browsing history keeps its high-water mark.
    const timer =
      request.page === 1 && request.snapshot === 0
        ? setInterval(() => void load(), 10000)
        : undefined;
    return () => {
      active = false;
      controller.abort();
      clearInterval(timer);
    };
  }, [path, request]);
  return {
    data,
    busy,
    error,
    refresh: () =>
      setRequest((r) => ({
        ...r,
        page: 1,
        snapshot: 0,
        revision: r.revision + 1,
      })),
    go: (page: number) =>
      setRequest((r) => ({ ...r, page, snapshot: data?.snapshot || 0 })),
    resize: (size: number) => {
      setData(null);
      setRequest((r) => ({ ...r, page: 1, size, snapshot: 0 }));
    },
  };
}

function Pager<T>({ records }: { records: ReturnType<typeof useRecords<T>> }) {
  const id = useId();
  const { data, busy, error, go, resize, refresh } = records;
  return (
    <>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="record-pagination" aria-label="记录分页">
        <span aria-live="polite">
          {data
            ? `共 ${data.total} 条，第 ${data.page} / ${data.pages} 页`
            : busy
              ? "正在读取记录…"
              : "记录读取失败"}
        </span>
        <label htmlFor={id}>
          每页{" "}
          <select
            id={id}
            value={data?.page_size || 10}
            disabled={busy}
            onChange={(e) => resize(Number(e.target.value))}
          >
            {[10, 20, 50, 100].map((n) => (
              <option key={n} value={n}>
                {n} 条
              </option>
            ))}
          </select>
        </label>
        <div className="row-actions">
          <button
            type="button"
            disabled={busy || !data || data.page <= 1}
            onClick={() => go(data!.page - 1)}
          >
            上一页
          </button>
          <button
            type="button"
            disabled={busy || !data || data.page >= data.pages}
            onClick={() => go(data!.page + 1)}
          >
            下一页
          </button>
          <button type="button" disabled={busy} onClick={refresh}>
            <RefreshCw aria-hidden="true" />
            刷新最新
          </button>
        </div>
      </div>
    </>
  );
}

export function ConnectionRecords({
  status,
  refresh,
  message,
}: {
  status: Status | null;
  refresh: () => Promise<void>;
  message: (value: string) => void;
}) {
  const records = useRecords<ConnectionRecord>("connections");
  const [draft, setDraft] = useState<NodeDraft | null>(null);
  return (
    <section aria-label="连接记录" aria-busy={records.busy}>
      <p className="list-hint">
        记录经过代理的请求。HTTP 显示完整 URL；HTTPS
        仅记录域名和端口，添加节点时需补充下载地址。
      </p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>来源设备 IP</th>
              <th>方法</th>
              <th>请求地址</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {records.data?.items.map((record) => {
              const rule = status?.rules.find(
                (rule) => rule.domain === record.domain,
              );
              const tunnel = record.method === "CONNECT";
              const eligible =
                tunnel || record.method === "GET" || record.method === "HEAD";
              const collected = !tunnel && rule?.url === record.address;
              return (
                <tr key={record.id}>
                  <td>{date(record.time)}</td>
                  <td>{record.client}</td>
                  <td>{record.method}</td>
                  <td className="connection-address">
                    {record.address}
                    {tunnel && <small>HTTPS 隧道，仅域名和端口</small>}
                  </td>
                  <td>
                    <button
                      type="button"
                      disabled={!status || !eligible || collected}
                      title={
                        !eligible
                          ? "仅 GET／HEAD 请求可直接作为下载样本"
                          : undefined
                      }
                      onClick={() =>
                        setDraft({
                          url: tunnel ? "" : record.address,
                          domain: record.domain,
                          platform: rule?.platform || "",
                          supplement: !!rule,
                        })
                      }
                    >
                      {collected
                        ? "已收录"
                        : tunnel
                          ? "补充下载地址"
                          : rule
                            ? "补充节点"
                            : "添加节点"}
                    </button>
                  </td>
                </tr>
              );
            })}
            {records.data?.items.length === 0 && (
              <tr>
                <td colSpan={5} className="empty">
                  暂无连接记录。主机使用代理发起下载后，记录会自动出现。
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <Pager records={records} />
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

export function QueryRecords() {
  const records = useRecords<Query>("queries");
  return (
    <section aria-label="查询记录" aria-busy={records.busy}>
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
            {records.data?.items.map((q) => (
              <tr key={q.id}>
                <td>{q.client}</td>
                <td>{q.domain}</td>
                <td>{q.type}</td>
                <td>{q.optimized ? "优选节点" : "上游解析"}</td>
                <td>{date(q.time)}</td>
              </tr>
            ))}
            {records.data?.items.length === 0 && (
              <tr>
                <td colSpan={5} className="empty">
                  暂无查询记录。设置主机 DNS 后重新执行网络测试。
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <Pager records={records} />
    </section>
  );
}

function Results({ filter }: { filter: string }) {
  const records = useRecords<Result>(`history?filter=${filter}`);
  return (
    <section aria-label="测速结果" aria-busy={records.busy}>
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
            {records.data?.items.map((r) => (
              <tr key={r.id}>
                <td>
                  <strong>{r.domain}</strong>
                  <small>{date(r.time)}</small>
                </td>
                <td>{r.ip}</td>
                <td>{r.kind === "health" ? "健康检查" : "吞吐测试"}</td>
                <td>
                  {r.ok ? (
                    <span className="success">
                      {r.kind === "throughput"
                        ? `${r.speed_mbps.toFixed(1)} Mbps`
                        : "可用"}
                    </span>
                  ) : (
                    <span className="error">{r.error}</span>
                  )}
                </td>
                <td>{bytes(r.bytes)}</td>
              </tr>
            ))}
            {records.data?.items.length === 0 && (
              <tr>
                <td colSpan={5} className="empty">
                  暂无符合条件的测速记录。
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <Pager records={records} />
    </section>
  );
}

export function Events() {
  const records = useRecords<EventRecord>("history/events");
  return (
    <section aria-label="运行日志" aria-busy={records.busy}>
      <ul className="event-list">
        {records.data?.items.map((e) => (
          <li key={e.id}>
            <time>{date(e.time)}</time>
            <span>{e.message}</span>
          </li>
        ))}
      </ul>
      {records.data?.items.length === 0 && (
        <p className="empty">暂无运行记录。</p>
      )}
      <Pager records={records} />
    </section>
  );
}

export function HistoryRecords() {
  const [filter, setFilter] = useState("all");
  const id = useId();
  const filters = [
    ["all", "全部探测"],
    ["throughput", "吞吐测试"],
    ["health", "健康检查"],
    ["failed", "失败记录"],
  ] as const;
  return (
    <>
      <div className="tabs" role="tablist" aria-label="测速记录筛选">
        {filters.map(([value, label], index) => (
          <button
            key={value}
            type="button"
            id={`${id}-${value}`}
            role="tab"
            aria-selected={filter === value}
            aria-controls={`${id}-panel`}
            tabIndex={filter === value ? 0 : -1}
            onClick={() => setFilter(value)}
            onKeyDown={(e) => {
              const next =
                e.key === "ArrowRight"
                  ? (index + 1) % filters.length
                  : e.key === "ArrowLeft"
                    ? (index + filters.length - 1) % filters.length
                    : e.key === "Home"
                      ? 0
                      : e.key === "End"
                        ? filters.length - 1
                        : -1;
              if (next < 0) return;
              e.preventDefault();
              setFilter(filters[next][0]);
              (
                e.currentTarget.parentElement?.children[
                  next
                ] as HTMLButtonElement
              )?.focus();
            }}
          >
            {label}
          </button>
        ))}
      </div>
      <div
        id={`${id}-panel`}
        role="tabpanel"
        aria-labelledby={`${id}-${filter}`}
        tabIndex={0}
      >
        <Results key={filter} filter={filter} />
      </div>
    </>
  );
}
