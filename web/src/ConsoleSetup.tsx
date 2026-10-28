import { useRef, useState } from "react";
import {
  ArrowRight,
  Check,
  Copy,
  ChevronDown,
  Gamepad2,
  Network,
} from "lucide-react";
import * as Select from "@radix-ui/react-select";
import { ProxySpeed } from "./ProxySpeed";

type Mode = "proxy" | "dns";
const modes = [
  ["proxy", "代理模式"],
  ["dns", "DNS模式"],
] as const;

const models = ["PS5", "PS4"] as const;

const guides = {
  PS5: {
    path: "设置 → 网络 → 设置 → 设定互联网连接",
    steps: [
      "选中正在使用的 Wi-Fi 或有线网络，打开高级设置。",
      "将 DNS 设置改为手动，主 DNS 填入“DNS模式”中的地址；IP 地址、MTU 和代理保持原设置。",
      "保存后测试互联网连接，再暂停并恢复下载，让主机重新解析。",
    ],
    source:
      "https://www.playstation.com/en-gb/support/connectivity/internet-connect-playstation/",
  },
  PS4: {
    path: "设置 → 网络 → 设定互联网连接 → Wi-Fi / LAN → 自定义",
    steps: [
      "选择当前网络，保留原有 IP 地址和 DHCP 设置。",
      "在 DNS 设置中选择手动，主 DNS 填入“DNS模式”中的地址，其他网络参数保持原设置。",
      "保存并测试互联网连接，再暂停并恢复下载。",
    ],
    source:
      "https://www.playstation.com/en-gb/support/connectivity/internet-connect-playstation/",
  },
  Xbox: {
    path: "设置 → 常规 → 网络设置 → 高级设置 → DNS 设置",
    steps: [
      "在 DNS 设置中选择手动；Xbox Series X|S 和 Xbox One 均从网络设置进入。",
      "主 DNS 填入“DNS模式”中的地址，备用 DNS 可留空，必须填写时使用相同地址；IP 地址设置保持原样。",
      "返回网络设置，测试网络连接，再暂停并恢复下载。",
    ],
    source:
      "https://www.epicgames.com/help/c-BillingSupport_Gifting/a10497311?lang=en-US",
  },
  Switch: {
    path: "系统设置 → 互联网 → 互联网设置 → 当前网络 → 更改设置",
    steps: [
      "选择正在使用的网络，找到 DNS 设置并改为手动。",
      "在首选 DNS 中填入“DNS模式”中的地址，备用 DNS 可留空，必须填写时使用相同地址，然后保存。",
      "连接此网络并执行连接测试，再重新开始下载。",
    ],
    source:
      "https://en-americas-support.nintendo.com/app/answers/detail/a_id/22411/",
  },
};

export function ConsoleSetup({
  addresses,
  proxyPort,
  message,
}: {
  addresses: string[];
  proxyPort: number;
  message: (value: string) => void;
}) {
  const [selectedAddress, setSelectedAddress] = useState("");
  const address = addresses.includes(selectedAddress)
    ? selectedAddress
    : addresses[0] || "";
  const [mode, setMode] = useState<Mode>("proxy");
  const [platform, setPlatform] = useState("PlayStation");
  const [model, setModel] = useState<"PS5" | "PS4">("PS5");
  const fields = useRef<Record<Mode, HTMLInputElement | null>>({
    proxy: null,
    dns: null,
  });
  const choices = ["PlayStation", "Xbox", "Switch"];
  const dnsGuide =
    guides[
      platform === "PlayStation" ? model : (platform as "Xbox" | "Switch")
    ];
  const proxySteps =
    platform === "Xbox"
      ? [
          "Xbox 请使用 DNS模式，切换下方配置模式查看设置步骤。",
          "只修改 DNS 不会让下载流量经过此代理。",
        ]
      : [
          platform === "Switch"
            ? "打开当前网络的更改设置，找到代理设置并选择使用。"
            : "打开当前网络的高级或自定义设置，在代理服务器中选择使用。",
          `服务器填写“代理模式”中的 IP，端口填写 ${proxyPort}；无需用户名或密码，也不需要安装证书。`,
          "保存后测试互联网连接，再重新建立下载连接。DNS 可保留原设置。",
        ];
  const guide =
    mode === "dns"
      ? dnsGuide
      : {
          path:
            platform === "Xbox"
              ? "Xbox 使用 DNS模式接入"
              : dnsGuide.path + " → 代理设置",
          steps: proxySteps,
          source:
            platform === "Switch"
              ? "https://en-americas-support.nintendo.com/app/answers/detail/a_id/22316/"
              : dnsGuide.source,
        };
  async function copy(target: Mode) {
    const value = address;
    if (!value) return;
    try {
      if (!navigator.clipboard?.writeText)
        throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(value);
      message("地址已复制");
    } catch {
      const focused = document.activeElement as HTMLElement | null;
      const field = fields.current[target] || document.createElement("input");
      if (!fields.current[target]) {
        field.value = value;
        field.style.cssText = "position:fixed;opacity:0;pointer-events:none";
        document.body.append(field);
      }
      field.focus();
      field.select();
      // Plain HTTP pages may lack Clipboard API access.
      let copied = false;
      try {
        copied = document.execCommand("copy");
      } catch {}
      if (!fields.current[target]) {
        field.remove();
        focused?.focus();
      }
      message(copied ? "地址已复制" : "请手动复制地址：" + value);
    }
  }
  return (
    <div className="connection-layout">
      <div className="connection-modes">
        {modes.map(([value, label]) => {
          const ip = address;
          return (
            <section
              className="mode-card setup-config"
              key={value}
              aria-labelledby={"mode-title-" + value}
            >
              <h2 id={"mode-title-" + value}>
                <Network aria-hidden="true" />
                {label}
              </h2>
              <p className="mode-benefit">
                {value === "proxy"
                  ? "新建连接及时跟随优选节点变化，无需等待主机 DNS 缓存更新。"
                  : "兼容 PlayStation、Xbox、Switch，下载流量直连节点。"}
              </p>
              <label htmlFor={"console-" + value}>
                {value === "dns" ? "主 DNS / 首选 DNS" : "代理服务器"}
              </label>
              {ip ? (
                <>
                  <div className="dns-copy">
                    {addresses.length > 1 ? (
                      <Select.Root
                        value={address}
                        onValueChange={(ip) => {
                          setSelectedAddress(ip);
                        }}
                      >
                        <Select.Trigger
                          id={"console-" + value}
                          className="select-trigger address-select"
                        >
                          <Select.Value />
                          <Select.Icon>
                            <ChevronDown aria-hidden="true" />
                          </Select.Icon>
                        </Select.Trigger>
                        <Select.Portal>
                          <Select.Content
                            className="select-content"
                            position="popper"
                            sideOffset={4}
                          >
                            <Select.Viewport>
                              {addresses.map((ip) => (
                                <Select.Item
                                  key={ip}
                                  value={ip}
                                  className="select-item"
                                >
                                  <Select.ItemText>{ip}</Select.ItemText>
                                  <Select.ItemIndicator>
                                    <Check aria-hidden="true" />
                                  </Select.ItemIndicator>
                                </Select.Item>
                              ))}
                            </Select.Viewport>
                          </Select.Content>
                        </Select.Portal>
                      </Select.Root>
                    ) : (
                      <input
                        id={"console-" + value}
                        ref={(element) => {
                          fields.current[value] = element;
                        }}
                        readOnly
                        value={ip}
                        onFocus={(e) => e.currentTarget.select()}
                        spellCheck={false}
                      />
                    )}
                    <button
                      type="button"
                      className="copy-address"
                      onClick={() => void copy(value)}
                      aria-label={
                        value === "dns" ? "复制 DNS 地址" : "复制代理地址"
                      }
                    >
                      <Copy aria-hidden="true" />
                    </button>
                  </div>
                </>
              ) : (
                <p className="setup-warning" role="status">
                  尚未配置主机可用的服务地址。请检查部署时声明的固定局域网 IP。
                </p>
              )}
              <dl className="setup-facts">
                {value === "proxy" && <ProxySpeed />}
                <div>
                  <dt>{value === "dns" ? "DNS 设置" : "代理设置"}</dt>
                  <dd>手动</dd>
                </div>
                {value === "proxy" && (
                  <div>
                    <dt>端口</dt>
                    <dd>{proxyPort}（TCP）</dd>
                  </div>
                )}
                {value === "proxy" && (
                  <div>
                    <dt>用户名 / 密码</dt>
                    <dd>不需要</dd>
                  </div>
                )}
                {value === "dns" && (
                  <div>
                    <dt>备用 DNS</dt>
                    <dd>留空或同主 DNS</dd>
                  </div>
                )}
              </dl>
            </section>
          );
        })}
      </div>
      <section className="console-setup" aria-labelledby="console-setup-title">
        <div className="setup-heading">
          <h2 id="console-setup-title">
            <Gamepad2 aria-hidden="true" />
            配置方式
          </h2>
        </div>
        <div
          className="tabs connection-tabs"
          role="tablist"
          aria-label="配置模式"
        >
          {modes.map(([value, label], index) => (
            <button
              key={value}
              type="button"
              id={"connection-tab-" + value}
              role="tab"
              aria-selected={mode === value}
              aria-controls="connection-panel"
              tabIndex={mode === value ? 0 : -1}
              onClick={() => {
                setMode(value);
              }}
              onKeyDown={(e) => {
                const next =
                  e.key === "ArrowRight" || e.key === "ArrowLeft"
                    ? (index + 1) % modes.length
                    : e.key === "Home"
                      ? 0
                      : e.key === "End"
                        ? modes.length - 1
                        : -1;
                if (next < 0) return;
                e.preventDefault();
                setMode(modes[next][0]);
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
          className="setup-grid"
          id="connection-panel"
          role="tabpanel"
          aria-labelledby={"connection-tab-" + mode}
          tabIndex={0}
        >
          <div className="setup-options">
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
            {platform === "PlayStation" && (
              <div className="guide-model">
                <div
                  className="tabs model-tabs"
                  role="tablist"
                  aria-label="主机型号"
                >
                  {models.map((value, index) => (
                    <button
                      type="button"
                      key={value}
                      id={"model-tab-" + value}
                      role="tab"
                      aria-selected={model === value}
                      aria-controls="model-guide-panel"
                      tabIndex={model === value ? 0 : -1}
                      onClick={() => setModel(value)}
                      onKeyDown={(e) => {
                        const next =
                          e.key === "ArrowRight" || e.key === "ArrowLeft"
                            ? (index + 1) % models.length
                            : e.key === "Home"
                              ? 0
                              : e.key === "End"
                                ? models.length - 1
                                : -1;
                        if (next < 0) return;
                        e.preventDefault();
                        setModel(models[next]);
                        (
                          e.currentTarget.parentElement?.children[
                            next
                          ] as HTMLButtonElement
                        )?.focus();
                      }}
                    >
                      {value}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>
          <div
            className="setup-guide"
            id="console-guide-panel"
            role="tabpanel"
            aria-labelledby={"guide-tab-" + choices.indexOf(platform)}
          >
            <div
              className="guide-content"
              key={mode + platform + model}
              id={platform === "PlayStation" ? "model-guide-panel" : undefined}
              role={platform === "PlayStation" ? "tabpanel" : undefined}
              aria-labelledby={
                platform === "PlayStation" ? "model-tab-" + model : undefined
              }
              tabIndex={platform === "PlayStation" ? 0 : undefined}
            >
              <p className="guide-path">{guide.path}</p>
              <ol className="guide-steps">
                {guide.steps.map((step) => (
                  <li key={step}>{step}</li>
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
      </section>
    </div>
  );
}
