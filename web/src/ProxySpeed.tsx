import { useEffect, useState } from "react";
import { api, downloadSpeed, type ProxyTraffic } from "./api";

export function ProxySpeed() {
  const [speed, setSpeed] = useState<number | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let previous: ProxyTraffic | null = null;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    let controller: AbortController;
    async function sample() {
      controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 5000);
      try {
        const current = await api<ProxyTraffic>(
          "proxy/traffic",
          undefined,
          "GET",
          controller.signal,
        );
        if (stopped) return;
        setSpeed(previous ? downloadSpeed(previous, current) : null);
        setFailed(false);
        previous = current;
      } catch {
        if (stopped) return;
        previous = null;
        setSpeed(null);
        setFailed(true);
      } finally {
        clearTimeout(timeout);
        if (!stopped) timer = setTimeout(() => void sample(), 1000);
      }
    }
    void sample();
    return () => {
      stopped = true;
      clearTimeout(timer);
      controller.abort();
    };
  }, []);
  return (
    <dl className="proxy-speed">
      <div>
        <dt>当前下载速度</dt>
        <dd>
          <output aria-label="当前下载速度" aria-live="off">
            {failed
              ? "暂不可用"
              : speed === null
                ? "正在采样…"
                : `${(speed / 1000000).toFixed(2)} MB/s`}
          </output>
        </dd>
      </div>
    </dl>
  );
}
