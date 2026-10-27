export type FeedStatus = {
  url: string;
  attempt: string;
  success: string;
  error?: string;
  version: string;
};
export type Rule = {
  id: string;
  platform: string;
  domain: string;
  url: string;
  enabled: boolean;
  candidates: string[];
  min_bytes: number;
  expected_prefix?: string;
};
export type Config = {
  enabled: boolean;
  proxy_enabled: boolean;
  upstreams: string[];
  allowed_cidrs: string[];
  probe_seconds: number;
  interval_seconds: number;
  health_seconds: number;
  policy_seconds: number;
  ttl: number;
  improvement: number;
  cooldown_seconds: number;
  timezone: string;
  rules: Rule[];
  rule_version: string;
  rule_source: string;
  feed_url: string;
  feed_seconds: number;
};
export type Selection = {
  rule_id: string;
  domain: string;
  family: string;
  ip: string;
  speed_mbps: number;
  valid_until: string;
  changed_at: string;
  checked_at: string;
  reason: string;
  fixed: boolean;
};
export type Task = {
  rule_id: string;
  kind: string;
  state: string;
  message: string;
  time: string;
};
export type Query = {
  id: number;
  domain: string;
  type: string;
  client: string;
  time: string;
  optimized: boolean;
};
export type Status = {
  app_version: string;
  app_revision: string;
  enabled: boolean;
  selections: Selection[];
  tasks: Task[];
  version: number;
  applied_version: number;
  dns_connected: boolean;
  dns_address?: string;
  proxy: {
    enabled: boolean;
    address: string;
    port: number;
    active_transfers: number;
    downloaded_bytes: number;
  };
  feed: FeedStatus;
  last_tests: Record<string, string>;
  uptime_seconds: number;
  rules: Rule[];
  interval_seconds: number;
  next_tests: Record<string, string>;
};
export type Result = {
  id: number;
  rule_id: string;
  domain: string;
  ip: string;
  family: string;
  kind: string;
  time: string;
  bytes: number;
  duration_ms: number;
  speed_mbps: number;
  ok: boolean;
  error?: string;
};
export async function api<T>(
  path: string,
  body?: unknown,
  method = body === undefined ? "GET" : "POST",
  signal?: AbortSignal,
): Promise<T> {
  const res = await fetch("/api/v1/" + path, {
    method,
    headers: { "Content-Type": "application/json", "X-DNS-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data.error || "请求失败，请重试");
  }
  return data as T;
}
export function bytes(n: number) {
  return n >= 1073741824
    ? (n / 1073741824).toFixed(1) + " GiB"
    : (n / 1048576).toFixed(1) + " MiB";
}
export function date(s: string) {
  return s
    ? new Date(s).toLocaleString("zh-CN", { hour12: false })
    : "尚无记录";
}
export function active(s: Selection) {
  return !!s.ip && new Date(s.valid_until).getTime() > Date.now();
}
