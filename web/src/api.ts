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
  upstreams: string[];
  allowed_cidrs: string[];
  probe_seconds: number;
  probe_bytes: number;
  daily_bytes: number;
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
};
export type Selection = {
  rule_id: string;
  domain: string;
  family: string;
  ip: string;
  backup?: string;
  speed_mbps: number;
  valid_until: string;
  changed_at: string;
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
  domain: string;
  type: string;
  client: string;
  time: string;
  optimized: boolean;
};
export type Status = {
  enabled: boolean;
  selections: Selection[];
  tasks: Task[];
  queries: Query[];
  version: number;
  applied_version: number;
  dns_connected: boolean;
  dns_address?: string;
  budget_used: number;
  budget_limit: number;
  budget_day: string;
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
): Promise<T> {
  const res = await fetch("/api/v1/" + path, {
    method,
    headers: { "Content-Type": "application/json", "X-DNS-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
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
