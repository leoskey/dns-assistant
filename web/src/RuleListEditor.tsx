import { Fragment, useState } from "react";
import { ChevronDown, ChevronUp, Plus } from "lucide-react";
import { type Rule } from "./api";

export function RuleListEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  const [expanded, setExpanded] = useState<number | null>(null);
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
    else parseError = "规则结构不完整，请在高级编辑中修正后保存。";
  } catch {
    parseError = "规则 JSON 格式无效，请在高级编辑中修正后保存。";
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
      <div className="table-wrap">
        <table className="rules-table">
          <thead>
            <tr>
              <th>平台 / 下载域名</th>
              <th>测试下载地址</th>
              <th>启用</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <Fragment key={i}>
                <tr>
                  <td>
                    <strong>{r.domain || "新下载域名"}</strong>
                    <small>
                      {r.platform} · {r.id}
                    </small>
                  </td>
                  <td className="sample-cell">
                    <span title={r.url}>{r.url || "尚未填写测试样本"}</span>
                    <small>{r.candidates.length} 个额外候选</small>
                  </td>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={"启用 " + (r.domain || "新规则")}
                      checked={r.enabled}
                      onChange={(e) => update(i, { enabled: e.target.checked })}
                    />
                  </td>
                  <td>
                    <div className="row-actions">
                      <button
                        type="button"
                        aria-expanded={expanded === i}
                        aria-controls={"edit-rule-" + i}
                        onClick={() => setExpanded(expanded === i ? null : i)}
                      >
                        {expanded === i ? (
                          <ChevronUp aria-hidden="true" />
                        ) : (
                          <ChevronDown aria-hidden="true" />
                        )}
                        {expanded === i ? "收起" : "编辑"}
                      </button>
                      <button
                        type="button"
                        className="quiet destructive"
                        onClick={() => {
                          if (
                            window.confirm(
                              "移除此下载域名的规则？保存后将恢复普通解析。",
                            )
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
                        移除
                      </button>
                    </div>
                  </td>
                </tr>
                {expanded === i && (
                  <tr className="expanded-row">
                    <td colSpan={4}>
                      <div id={"edit-rule-" + i} className="rule-editors">
                        <fieldset>
                          <legend>
                            {r.platform} · {r.domain || "新规则"}
                          </legend>
                          <div className="fields two-col">
                            <label>
                              平台
                              <select
                                value={r.platform}
                                onChange={(e) =>
                                  update(i, { platform: e.target.value })
                                }
                              >
                                {["Xbox", "PlayStation", "Switch"].map((p) => (
                                  <option key={p}>{p}</option>
                                ))}
                              </select>
                            </label>
                            <label>
                              规则名称
                              <input
                                required
                                value={r.id}
                                onChange={(e) =>
                                  update(i, { id: e.target.value })
                                }
                              />
                            </label>
                          </div>
                          <label>
                            下载域名
                            <input
                              required
                              value={r.domain}
                              onChange={(e) =>
                                update(i, {
                                  domain: e.target.value.toLowerCase().trim(),
                                })
                              }
                            />
                          </label>
                          <label>
                            该域名的测试下载地址
                            <input
                              type="url"
                              required
                              value={r.url}
                              onChange={(e) =>
                                update(i, { url: e.target.value })
                              }
                            />
                          </label>
                          <label>
                            额外候选 IP（每行一个，可留空）
                            <textarea
                              rows={3}
                              value={r.candidates.join("\n")}
                              onChange={(e) =>
                                update(i, {
                                  candidates: e.target.value
                                    .split("\n")
                                    .filter(Boolean),
                                })
                              }
                            />
                          </label>
                          <details>
                            <summary>内容验证</summary>
                            <label>
                              最少有效字节数
                              <input
                                type="number"
                                min={1024}
                                value={r.min_bytes}
                                onChange={(e) =>
                                  update(i, {
                                    min_bytes: Number(e.target.value),
                                  })
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
                          </details>
                        </fieldset>
                      </div>
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
            {!rows.length && (
              <tr>
                <td colSpan={4} className="empty">
                  没有下载规则。添加下载域名和有效内容样本后保存。
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <div className="list-toolbar">
        <button
          type="button"
          onClick={() => {
            const index = rows.length;
            onChange(
              JSON.stringify(
                [
                  ...rows,
                  {
                    id: "rule-" + Date.now(),
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
            setExpanded(index);
          }}
        >
          <Plus aria-hidden="true" />
          添加下载域名
        </button>
        <span>修改列表后，点击下方“保存设置”统一应用。</span>
      </div>
      <details>
        <summary>高级：编辑完整规则</summary>
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
