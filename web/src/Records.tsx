import { useEffect, useId, useState } from "react";
import { RefreshCw } from "lucide-react";
import { api, bytes, date, type Result, type Query } from "./api";

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
    size: 20,
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
            ? `共 ${data.total} 条 · 第 ${data.page} / ${data.pages} 页`
            : busy
              ? "正在读取记录…"
              : "记录读取失败"}
        </span>
        <label htmlFor={id}>
          每页{" "}
          <select
            id={id}
            value={data?.page_size || 20}
            disabled={busy}
            onChange={(e) => resize(Number(e.target.value))}
          >
            {[20, 50, 100].map((n) => (
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
      {data && (
        <p className="record-retention">
          保留最近 {data.retention_days} 天，最多{" "}
          {data.max_records.toLocaleString()} 条；较早记录自动清理。
        </p>
      )}
    </>
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
    <section aria-label="运行与切换记录" aria-busy={records.busy}>
      <h2 className="section-heading">运行与切换记录</h2>
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
  return (
    <>
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
      <Results key={filter} filter={filter} />
    </>
  );
}
