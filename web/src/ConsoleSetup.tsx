import { useEffect, useRef, useState } from "react";
import { NavLink } from "react-router-dom";
import { ArrowRight, Check, Copy, Gamepad2, Network } from "lucide-react";
import type { Status } from "./api";

const guides = {
  PS5: {
    path: "设置 → 网络 → 设置 → 设定互联网连接",
    steps: [
      "选中正在使用的 Wi-Fi 或有线网络，打开高级设置。",
      "将 DNS 设置改为手动，主 DNS 填入左侧地址；IP 地址、MTU 和代理保持原设置。",
      "保存后测试互联网连接，再暂停并恢复下载，让主机重新解析。",
    ],
    source:
      "https://www.playstation.com/en-gb/support/connectivity/internet-connect-playstation/",
  },
  PS4: {
    path: "设置 → 网络 → 设定互联网连接 → Wi-Fi / LAN → 自定义",
    steps: [
      "选择当前网络，保留原有 IP 地址和 DHCP 设置。",
      "在 DNS 设置中选择手动，主 DNS 填入左侧地址，其他网络参数保持原设置。",
      "保存并测试互联网连接，再暂停并恢复下载。",
    ],
    source:
      "https://www.playstation.com/en-gb/support/connectivity/internet-connect-playstation/",
  },
  Xbox: {
    path: "设置 → 常规 → 网络设置 → 高级设置 → DNS 设置",
    steps: [
      "在 DNS 设置中选择手动；Xbox Series X|S 和 Xbox One 均从网络设置进入。",
      "主 DNS 填入左侧地址，备用 DNS 按下方说明填写；IP 地址设置保持原样。",
      "返回网络设置，测试网络连接，再暂停并恢复下载。",
    ],
    source:
      "https://www.epicgames.com/help/c-BillingSupport_Gifting/a10497311?lang=en-US",
  },
  Switch: {
    path: "系统设置 → 互联网 → 互联网设置 → 当前网络 → 更改设置",
    steps: [
      "选择正在使用的网络，找到 DNS 设置并改为手动。",
      "在首选 DNS 中填入左侧地址，备用 DNS 按下方说明填写，然后保存。",
      "连接此网络并执行连接测试，再重新开始下载。",
    ],
    source:
      "https://en-americas-support.nintendo.com/app/answers/detail/a_id/22411/",
  },
};

export function ConsoleSetup({
  address,
  connected,
  proxy,
}: {
  address?: string;
  connected: boolean;
  proxy: Status["proxy"];
}) {
  const [mode, setMode] = useState<"dns" | "proxy">("dns");
  const [platform, setPlatform] = useState("PlayStation");
  const [model, setModel] = useState<"PS5" | "PS4">("PS5");
  const [feedback, setFeedback] = useState("");
  const field = useRef<HTMLInputElement>(null);
  const choices = ["PlayStation", "Xbox", "Switch"];
  const dnsGuide =
    guides[
      platform === "PlayStation" ? model : (platform as "Xbox" | "Switch")
    ];
  const proxySteps =
    platform === "Xbox"
      ? [
          "Xbox 请使用 DNS 优选，切换接入方式后查看设置步骤。",
          "只修改 DNS 不会让下载流量经过此代理。",
        ]
      : [
          platform === "Switch"
            ? "打开当前网络的更改设置，找到代理设置并选择使用。"
            : "打开当前网络的高级或自定义设置，在代理服务器中选择使用。",
          "服务器填写上方 IP，端口填写 8082；无需用户名或密码，也不需要安装证书。",
          "保存后测试互联网连接，再重新建立下载连接。DNS 可保留原设置。",
        ];
  const guide =
    mode === "dns"
      ? dnsGuide
      : {
          path:
            platform === "Xbox"
              ? "Xbox 使用 DNS 优选接入"
              : dnsGuide.path + " → 代理设置",
          steps: proxySteps,
          source:
            platform === "Switch"
              ? "https://en-americas-support.nintendo.com/app/answers/detail/a_id/22316/"
              : dnsGuide.source,
        };
  useEffect(() => {
    if (!feedback) return;
    const timer = setTimeout(() => setFeedback(""), 4000);
    return () => clearTimeout(timer);
  }, [feedback]);
  async function copy() {
    if (!address) return;
    try {
      if (!navigator.clipboard?.writeText)
        throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(address);
      setFeedback("地址已复制");
    } catch {
      field.current?.focus();
      field.current?.select();
      // Plain HTTP pages may lack Clipboard API access; retain selection if copying is denied.
      let copied = false;
      try {
        copied = document.execCommand("copy");
      } catch {}
      setFeedback(
        copied ? "地址已复制" : "地址已选中，请长按或按 Ctrl/Cmd+C 复制",
      );
    }
  }
  return (
    <section className="console-setup" aria-labelledby="console-setup-title">
      <div className="setup-heading">
        <div>
          <h2 id="console-setup-title">
            <Gamepad2 aria-hidden="true" />
            连接你的主机
          </h2>
          <p>让主机和运行服务的设备连接同一局域网，选择一种接入方式。</p>
        </div>
        <NavLink className="button" to={mode === "dns" ? "/connection" : "/settings"}>
          <Network aria-hidden="true" />
          {mode === "dns" ? "检查接入" : "代理设置"}
        </NavLink>
      </div>
      <div className="setup-grid">
        <div className="setup-config">
          <label className="guide-model">
            接入方式
            <select
              aria-label="接入方式"
              value={mode}
              onChange={(e) => {
                setMode(e.target.value as "dns" | "proxy");
                setFeedback("");
              }}
            >
              <option value="dns">DNS 优选</option>
              <option value="proxy">下载代理</option>
            </select>
          </label>
          <label htmlFor="console-dns">
            {mode === "dns" ? "主 DNS / 首选 DNS" : "代理服务器"}
          </label>
          {address ? (
            <>
              <div className="dns-copy">
                <input
                  id="console-dns"
                  ref={field}
                  readOnly
                  value={address}
                  onFocus={(e) => e.currentTarget.select()}
                  spellCheck={false}
                />
                <button
                  type="button"
                  onClick={() => void copy()}
                  aria-label={mode === "dns" ? "复制 DNS 地址" : "复制代理地址"}
                >
                  {feedback === "地址已复制" ? (
                    <Check aria-hidden="true" />
                  ) : (
                    <Copy aria-hidden="true" />
                  )}
                  复制
                </button>
              </div>
              <p className="copy-feedback" role="status" aria-live="polite">
                {feedback || "仅填写 IP 地址，不加 http:// 或端口。"}
              </p>
            </>
          ) : (
            <p className="setup-warning" role="status">
              尚未配置主机可用的服务地址。请检查部署时声明的固定局域网 IP。
            </p>
          )}
          <dl className="setup-facts">
            <div>
              <dt>{mode === "dns" ? "DNS 设置" : "代理设置"}</dt>
              <dd>手动</dd>
            </div>
            <div>
              <dt>标准端口</dt>
              <dd>
                {mode === "dns" ? "53（UDP / TCP）" : `${proxy.port}（TCP）`}
              </dd>
            </div>
            <div>
              <dt>服务状态</dt>
              <dd>
                <span
                  className={
                    "dot " +
                    ((mode === "dns" ? connected : proxy.enabled) ? "on" : "")
                  }
                />
                {mode === "dns"
                  ? connected
                    ? "DNS 核心已连接"
                    : "DNS 核心尚未连接"
                  : proxy.enabled
                    ? "下载代理已启用"
                    : "下载代理未启用"}
              </dd>
            </div>
          </dl>
          {mode === "dns" ? (
            <details className="secondary-guide">
              <summary>备用 DNS 怎么填？</summary>
              <p>
                允许留空时留空。必须填写且主机允许相同地址时，可填入上方
                IP；这不提供另一台服务器的故障冗余。
              </p>
              <p>
                也可使用另一台运行相同规则的局域网 DNS。填写公共备用 DNS
                可能让部分查询绕过选优。
              </p>
            </details>
          ) : (
            <p className="muted">
              使用优选节点不需要再修改主机
              DNS。普通目标正常转发，代理不会自动接管未配置设备的流量。
            </p>
          )}
          {mode === "dns" && !connected && (
            <p className="setup-warning">
              先确认 DNS 服务已启动，再修改主机设置。
            </p>
          )}
        </div>
        <div className="setup-guide">
          <div
            className="tabs guide-tabs"
            role="tablist"
            aria-label="主机设置指引"
          >
            {choices.map((p, index) => (
              <button
                key={p}
                id={"guide-tab-" + index}
                role="tab"
                aria-selected={platform === p}
                aria-controls="console-guide-panel"
                tabIndex={platform === p ? 0 : -1}
                onClick={() => setPlatform(p)}
                onKeyDown={(e) => {
                  const next =
                    e.key === "ArrowRight"
                      ? (index + 1) % 3
                      : e.key === "ArrowLeft"
                        ? (index + 2) % 3
                        : e.key === "Home"
                          ? 0
                          : e.key === "End"
                            ? 2
                            : -1;
                  if (next < 0) return;
                  e.preventDefault();
                  setPlatform(choices[next]);
                  (
                    e.currentTarget.parentElement?.children[
                      next
                    ] as HTMLButtonElement
                  )?.focus();
                }}
              >
                {p}
              </button>
            ))}
          </div>
          <div
            id="console-guide-panel"
            role="tabpanel"
            aria-labelledby={"guide-tab-" + choices.indexOf(platform)}
          >
            {platform === "PlayStation" && (
              <label className="guide-model">
                主机型号
                <select
                  value={model}
                  onChange={(e) => setModel(e.target.value as "PS5" | "PS4")}
                >
                  <option>PS5</option>
                  <option>PS4</option>
                </select>
              </label>
            )}
            <div className="guide-content" key={mode + platform + model}>
              <p className="guide-path">{guide.path}</p>
              <ol className="guide-steps">
                {guide.steps.map((step) => (
                  <li key={step}>{step.replace("左侧", "上方")}</li>
                ))}
              </ol>
              {!(mode === "proxy" && platform === "Xbox") && (
                <a
                  className="guide-source"
                  href={guide.source}
                  target="_blank"
                  rel="noreferrer"
                >
                  {platform === "Xbox" ? "Epic Games 设置说明" : "官方设置说明"}
                  <ArrowRight aria-hidden="true" />
                </a>
              )}
            </div>
          </div>
        </div>
      </div>
      <div className="setup-footer">
        <p>
          {mode === "dns"
            ? "保存后，在“接入检查”确认出现主机或路由器的查询。"
            : "开始下载后，查看首页的转发数量与累计下行流量，确认流量经过代理。"}
          网络测试通过不代表游戏下载已提速。
        </p>
        <p>
          {mode === "dns"
            ? "服务停机或无法联网时，将主机 DNS 改回自动并重新测试。"
            : "代理关闭或服务停机前，将主机代理设置改为不使用，再测试网络连接。"}
          菜单名称可能随系统语言和版本变化。
        </p>
      </div>
    </section>
  );
}
