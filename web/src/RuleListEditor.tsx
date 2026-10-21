import { useState } from "react";
import { ChevronDown, ChevronUp, Plus } from "lucide-react";
import { type Rule } from "./api";

export function domainFromURL(value: string): string {
  try {
    return new URL(value).hostname.toLowerCase().replace(/\.$/, "");
  } catch {
    return "";
  }
}

export function RuleListEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  const [expanded, setExpanded] = useState<string | null>(null);
  let rows: Rule[] = [];
  let parseError = "";
  try {
    const parsed = JSON.parse(value);
    if (
      Array.isArray(parsed) &&
      parsed.every(
        (r) =>
          r &&
          typeof r.id === "string" &&
          typeof r.platform === "string" &&
          typeof r.domain === "string" &&
          typeof r.url === "string" &&
          typeof r.enabled === "boolean" &&
          Array.isArray(r.candidates) &&
          r.candidates.every((ip: unknown) => typeof ip === "string") &&
          typeof r.min_bytes === "number",
      )
    )
      rows = parsed;
    else parseError = "规则结构不完整，请在高级编辑中修正。";
  } catch {
    parseError = "规则 JSON 格式无效，请在高级编辑中修正。";
  }
  function update(index: number, patch: Partial<Rule>) {
    onChange(
      JSON.stringify(
        rows.map((r, i) => (i === index ? { ...r, ...patch } : r)),
        null,
        2,
      ),
    );
  }
  return (
    <>
      {parseError && (
        <p className="error" role="alert">
          {parseError}
        </p>
      )}
      <div className="download-rules">
        {rows.map((r, i) => {
          const open = expanded === r.id;
          return (
            <article className="download-rule" key={`${i}-${r.id}`}>
              <div className="download-rule-heading">
                <div>
                  <strong>{r.domain || "新下载域名"}</strong>
                  <small>{r.platform}</small>
                </div>
                <label className="checkbox">
                  <input
                    type="checkbox"
                    checked={r.enabled}
                    onChange={(e) => update(i, { enabled: e.target.checked })}
                  />
                  启用
                </label>
                <button
                  type="button"
                  className="quiet"
                  aria-expanded={open}
                  aria-controls={`rule-editor-${r.id}`}
                  onClick={() => setExpanded(open ? null : r.id)}
                >
                  {open ? (
                    <ChevronUp aria-hidden="true" />
                  ) : (
                    <ChevronDown aria-hidden="true" />
                  )}
                  {open ? "收起" : "编辑"}
                </button>
              </div>
              {!open && (
                <p className="rule-sample" title={r.url}>
                  {r.url || "请填写测试下载地址"}
                </p>
              )}
              {open && (
                <div
                  id={`rule-editor-${r.id}`}
                  className="fields rule-simple-editor"
                >
                  <label>
                    平台
                    <select
                      value={r.platform}
                      onChange={(e) => update(i, { platform: e.target.value })}
                    >
                      {["Xbox", "PlayStation", "Switch"].map((p) => (
                        <option key={p}>{p}</option>
                      ))}
                    </select>
                  </label>
                  <label>
                    测试下载地址
                    <input
                      type="url"
                      required
                      value={r.url}
                      placeholder="http://下载域名/测试文件"
                      onChange={(e) =>
                        update(i, {
                          url: e.target.value,
                          domain: domainFromURL(e.target.value),
                        })
                      }
                    />
                  </label>
                  <p className="muted">
                    下载域名：{r.domain || "从地址自动识别"}
                  </p>
                  <details>
                    <summary>高级选项</summary>
                    <div className="fields">
                      <label>
                        额外候选 IP（每行一个，可留空）
                        <textarea
                          rows={3}
                          value={r.candidates.join("\n")}
                          onChange={(e) =>
                            update(i, {
                              candidates: e.target.value
                                .split("\n")
                                .map((ip) => ip.trim())
                                .filter(Boolean),
                            })
                          }
                        />
                      </label>
                      <label>
                        最少有效字节数
                        <input
                          type="number"
                          min={1024}
                          value={r.min_bytes}
                          onChange={(e) =>
                            update(i, { min_bytes: Number(e.target.value) })
                          }
                        />
                      </label>
                      <label>
                        预期内容前缀（可留空）
                        <input
                          value={r.expected_prefix || ""}
                          onChange={(e) =>
                            update(i, { expected_prefix: e.target.value })
                          }
                        />
                      </label>
                    </div>
                  </details>
                  <button
                    type="button"
                    className="quiet destructive"
                    onClick={() => {
                      if (
                        window.confirm("移除此下载规则？保存后将恢复普通解析。")
                      ) {
                        onChange(
                          JSON.stringify(
                            rows.filter((_, index) => index !== i),
                            null,
                            2,
                          ),
                        );
                        setExpanded(null);
                      }
                    }}
                  >
                    移除规则
                  </button>
                </div>
              )}
            </article>
          );
        })}
        {!rows.length && !parseError && (
          <p className="empty">暂无下载规则，添加测试下载地址后保存。</p>
        )}
      </div>
      <div className="list-toolbar">
        <button
          type="button"
          disabled={!!parseError}
          onClick={() => {
            const id =
              "rule-" +
              Date.now().toString(36) +
              "-" +
              Math.random().toString(36).slice(2, 10);
            onChange(
              JSON.stringify(
                [
                  ...rows,
                  {
                    id,
                    platform: "Xbox",
                    domain: "",
                    url: "",
                    enabled: true,
                    candidates: [],
                    min_bytes: 65536,
                  },
                ],
                null,
                2,
              ),
            );
            setExpanded(id);
          }}
        >
          <Plus aria-hidden="true" />
          添加下载规则
        </button>
      </div>
      <details>
        <summary>高级：编辑完整规则 JSON</summary>
        <label>
          规则 JSON
          <textarea
            className="rules-editor"
            spellCheck={false}
            rows={18}
            value={value}
            onChange={(e) => onChange(e.target.value)}
          />
        </label>
      </details>
    </>
  );
}
