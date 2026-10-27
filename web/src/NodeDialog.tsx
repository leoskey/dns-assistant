import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Check, ChevronDown, X } from "lucide-react";
import * as Select from "@radix-ui/react-select";
import { api, type Config, type Rule } from "./api";
import { domainFromURL } from "./RuleListEditor";

export type NodeDraft = {
  url: string;
  platform: string;
  domain?: string;
  supplement?: boolean;
};

export function NodeDialog({
  draft,
  onClose,
  refresh,
  message,
}: {
  draft: NodeDraft;
  onClose: () => void;
  refresh: () => Promise<void>;
  message: (value: string) => void;
}) {
  const id = useId();
  const platforms = ["Xbox", "PlayStation", "Switch"];
  const addDialog = useRef<HTMLDialogElement>(null);
  const urlInput = useRef<HTMLInputElement>(null);
  const [url, setURL] = useState(draft.url);
  const [platform, setPlatform] = useState(draft.platform);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    addDialog.current?.showModal();
    urlInput.current?.focus();
  }, []);
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const domain = domainFromURL(url);
      if (!domain || !/^https?:\/\//i.test(url))
        throw new Error("请填写完整的 HTTP(S) 下载 URL");
      if (draft.domain && domain !== draft.domain)
        throw new Error("请填写记录中相同域名的下载 URL");
      if (!platform) throw new Error("请选择平台");
      const c = await api<Config>("config");
      const index = c.rules.findIndex((r) => r.domain === domain);
      if (index >= 0) {
        if (!draft.supplement) throw new Error("该域名已存在");
        c.rules[index] = { ...c.rules[index], url };
      } else {
        const rule: Rule = {
          id:
            "rule-" +
            Date.now().toString(36) +
            "-" +
            Math.random().toString(36).slice(2, 10),
          platform,
          domain,
          url,
          enabled: true,
          candidates: [],
          min_bytes: 65536,
        };
        c.rules.push(rule);
      }
      await api("config", c, "PUT");
      await refresh();
      close();
      message(draft.supplement ? "节点已补充" : "节点已添加");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function close() {
    addDialog.current?.close();
    onClose();
  }
  return (
    <dialog
      ref={addDialog}
      className="dns-check-dialog"
      aria-labelledby={`${id}-title`}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) close();
      }}
      onClick={(e) => {
        if (!busy && e.target === e.currentTarget) {
          const rect = e.currentTarget.getBoundingClientRect();
          if (
            e.clientX < rect.left ||
            e.clientX > rect.right ||
            e.clientY < rect.top ||
            e.clientY > rect.bottom
          )
            close();
        }
      }}
    >
      <div className="dns-check-heading">
        <h2 id={`${id}-title`}>{draft.supplement ? "补充节点" : "添加节点"}</h2>
        <button
          type="button"
          aria-label="关闭弹窗"
          disabled={!!busy}
          onClick={() => close()}
        >
          <X aria-hidden="true" />
        </button>
      </div>
      {draft.domain && !draft.url && (
        <p className="list-hint">
          HTTPS 仅记录目标 {draft.domain}，请补充该域名的完整下载 URL。
        </p>
      )}
      {draft.supplement && (
        <p className="list-hint">
          保存后替换该域名的测速 URL，保留原平台、候选 IP 与其他规则设置。
        </p>
      )}
      <form onSubmit={save}>
        <fieldset className="fields settings-fields" disabled={!!busy}>
          <div className="select-field">
            <label htmlFor={`${id}-add-platform`}>平台</label>
            <Select.Root
              value={platform}
              onValueChange={setPlatform}
              disabled={busy || !!draft.supplement}
            >
              <Select.Trigger
                id={`${id}-add-platform`}
                className="select-trigger"
              >
                <Select.Value placeholder="选择平台" />
                <Select.Icon>
                  <ChevronDown aria-hidden="true" />
                </Select.Icon>
              </Select.Trigger>
              {/* Keep the menu inside the native dialog's top layer. */}
              <Select.Content
                className="select-content"
                position="popper"
                align="start"
                sideOffset={4}
                collisionPadding={8}
              >
                <Select.Viewport>
                  {platforms.map((p) => (
                    <Select.Item className="select-item" key={p} value={p}>
                      <Select.ItemText>{p}</Select.ItemText>
                      <Select.ItemIndicator>
                        <Check size={15} aria-hidden="true" />
                      </Select.ItemIndicator>
                    </Select.Item>
                  ))}
                </Select.Viewport>
              </Select.Content>
            </Select.Root>
          </div>
          <label>
            测试下载 URL
            <input
              ref={urlInput}
              type="url"
              required
              value={url}
              onChange={(e) => setURL(e.target.value)}
              placeholder="https://下载域名/文件路径"
            />
          </label>
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
          <div className="row-actions">
            <button className="primary">
              {busy ? "保存中…" : draft.supplement ? "更新" : "保存"}
            </button>
            <button type="button" onClick={() => close()}>
              取消
            </button>
          </div>
        </fieldset>
      </form>
    </dialog>
  );
}
