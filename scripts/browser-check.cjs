// Opt-in end-to-end test against the actual Go/SQLite/DNS fixture.
// Set APP_TEST_BINARY, PLAYWRIGHT_MODULE, CHROME_PATH and BROWSER_OUTPUT_DIR.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const { spawn } = require("node:child_process");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const binary = process.env.APP_TEST_BINARY;
if (!binary) throw new Error("APP_TEST_BINARY must point to go test -c output");
const output =
  process.env.BROWSER_OUTPUT_DIR || path.resolve("work/browser-check");
fs.mkdirSync(output, { recursive: true });
const child = spawn(binary, ["-test.run", "^TestBrowserFixture$", "-test.v"], {
  env: {
    ...process.env,
    DNS_ASSISTANT_BROWSER_FIXTURE: "1",
    SERVER_IP: "192.168.1.10",
  },
  stdio: ["ignore", "pipe", "pipe"],
});
let log = "";
child.stderr.on("data", (b) => {
  log += b;
});
const exited = new Promise((resolve) =>
  child.on("exit", (code) => resolve(code)),
);
async function poll(fn, message, timeout = 15000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(message);
}
(async () => {
  let browser, base;
  try {
    base = await new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error("fixture startup timeout: " + log)),
        20000,
      );
      child.stdout.on("data", (b) => {
        log += b;
        const found = log.match(/BROWSER_FIXTURE_URL=(http:\/\/[^\s]+)/);
        if (found) {
          clearTimeout(timer);
          resolve(found[1]);
        }
      });
      child.on("exit", (code) => {
        clearTimeout(timer);
        reject(new Error("fixture exited: " + code + "\n" + log));
      });
    });
    const request = async (route, payload, method = payload === undefined ? "GET" : "POST") => {
      const r = await fetch(base + "/api/v1/" + route, { method, headers: payload === undefined ? {} : { "Content-Type": "application/json", "X-DNS-Request": "1" }, body: payload === undefined ? undefined : JSON.stringify(payload) });
      const body = await r.json();
      assert(r.ok, JSON.stringify(body));
      return body;
    };
    const fixture = async (route) => {
      const r = await fetch(base + "/fixture/" + route, { method: "POST" });
      assert(r.ok, await r.text());
    };
    const active = (s) => !!s.ip && Date.parse(s.valid_until) > Date.now();
    await poll(async () => {
      const s = await request("status");
      return (
        s.selections.filter(active).length === 2 &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "startup automatic selection failed");
    browser = await chromium.launch({
      headless: true,
      ...(process.env.CHROME_PATH
        ? { executablePath: process.env.CHROME_PATH }
        : {}),
    });
    const page = await browser.newPage({
      viewport: { width: 1280, height: 800 },
      reducedMotion: "reduce",
    });
    page.setDefaultTimeout(15000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(base + "/platforms");
    const row = () => page.locator(".platform-table tbody > tr").first();
    await poll(async () => {
      const s = await request("status");
      return (
        s.selections.filter(active).length === 2 &&
        s.version === s.applied_version &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "retest not applied");
    await row().getByText("健康", { exact: true }).waitFor();
    const tabs = page.getByRole("tablist", { name: "游戏平台" });
    await tabs.getByRole("tab", { name: "全部", exact: true }).focus();
    for (const [key, name] of [
      ["ArrowRight", "Xbox"],
      ["End", "Switch"],
      ["Home", "全部"],
    ]) {
      await page.keyboard.press(key);
      await poll(
        async () =>
          (await tabs
            .getByRole("tab", { name, exact: true })
            .getAttribute("aria-selected")) === "true",
        `keyboard ${key} did not select ${name}`,
      );
    }
    assert.equal(await page.getByText("IPv6", { exact: true }).count(), 0);
    const initial = await request("status");
    const id = initial.rules[0].id;
    await request("probe", { rule_id: id, ip: "1.1.1.1" });
    await poll(async () => {
      const state = await request("status");
      return state.selections.some(selection => selection.fixed && active(selection)) && state.version === state.applied_version;
    }, "API pin did not apply");
    await fixture("sync-off");
    await request("default", { rule_id: id });
    await poll(async () => { const state = await request("status"); return state.version !== state.applied_version; }, "sync failure was not retained");
    await row().getByText("默认解析", { exact: true }).waitFor();
    await fixture("sync-on");
    await poll(async () => { const state = await request("status"); return state.version === state.applied_version; }, "default did not synchronize");
    assert(!(await request("status")).selections.some(selection => selection.rule_id === id && active(selection)));
    await row().getByRole("button", { name: "测速", exact: true }).click();
    await poll(async () => {
      const s = await request("status");
      return (
        s.selections.filter(active).length === 2 &&
        s.version === s.applied_version &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "retest not applied");
    await row().getByText("健康", { exact: true }).waitFor();
    await fixture("fail-health");
    await poll(
      async () =>
        !(await request("status")).selections.some(
          (s) => s.rule_id === id && active(s),
        ),
      "health failure did not restore default",
    );
    await poll(
      async () =>
        (await request("status")).tasks.some(
          (t) => t.rule_id === id && t.state === "error",
        ),
      "failed recovery did not settle",
    );
    await fixture("recover");
    await row().getByRole("button", { name: "测速", exact: true }).click();
    await poll(async () => {
      const s = await request("status");
      return (
        s.selections.filter(active).length === 2 &&
        s.version === s.applied_version &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "retest not applied");
    await row().getByText("健康", { exact: true }).waitFor();
    await page.screenshot({ path: path.join(output, "platforms-desktop.png") });
    await page.getByRole("link", { name: "解析历史", exact: true }).click();
    assert.equal(await page.locator("#query-type").count(), 0);
    await page.getByRole("button", { name: "检查实际 DNS 应答", exact: true }).click();
    await page.getByRole("button", { name: "检查解析", exact: true }).click();
    await page
      .locator(".check-result")
      .getByText(/NOERROR/)
      .waitFor();
    assert.match(await page.locator(".check-result").innerText(), /1\.1\.1\.1/);
    await page.getByRole("dialog").getByRole("button", { name: "关闭弹窗", exact: true }).click();
    const queries = page.getByRole("region", { name: "查询记录" });
    assert.equal(await queries.getByLabel("每页").inputValue(), "10");
    await queries.getByRole("button", { name: "下一页", exact: true }).click();
    await queries.getByText(/第 2 \/ /).waitFor();
    await queries
      .getByRole("button", { name: "刷新最新", exact: true })
      .click();
    await queries.getByLabel("每页").selectOption("50");
    await queries.getByText(/第 1 \/ 1 页/).waitFor();
    await page.getByRole("link", { name: "测速记录", exact: true }).click();
    assert((await request("history/daily")).items.length > 0, "daily summary API must remain available");
    const recordTabs = page.getByRole("tablist", { name: "测速记录筛选" });
    await recordTabs.getByRole("tab", { name: "全部探测", exact: true }).press("End");
    assert.equal(
      await recordTabs.getByRole("tab", { name: "失败记录", exact: true }).getAttribute("aria-selected"),
      "true",
    );
    const results = page.getByRole("region", { name: "测速结果" });
    await poll(
      async () =>
        (await results.locator("tbody").innerText()).includes("HTTP 503"),
      "server-side failed filter missing",
    );
    assert(!(await results.locator("tbody").innerText()).includes(" Mbps"));
    await page.screenshot({ path: path.join(output, "records-desktop.png") });
    await page.getByRole("link", { name: "设置", exact: true }).click();
    const save = page.getByRole("button", { name: "保存设置", exact: true });
    await save.waitFor();
    const appVersion = (await request("status")).app_version;
    assert(appVersion, "application version missing from status");
    assert(
      (await page.getByTestId("app-version").innerText()).includes(
        `v${appVersion}`,
      ),
      "sidebar footer must display the running backend version",
    );
    assert(await save.isDisabled());
    assert.equal(await page.getByRole("switch").count(), 0, "settings must not expose a proxy switch");
    await page.getByRole("link", { name: "网络总览", exact: true }).click();
    const proxyCard = page.getByRole("region", { name: "代理模式", exact: true });
    const dnsCard = page.getByRole("region", { name: "DNS模式", exact: true });
    await proxyCard.waitFor();
    await dnsCard.waitFor();
    assert.equal(await page.getByRole("switch").count(), 0, "proxy must start without a switch");
    const beforeProxy = await request("config");
    assert(!Object.hasOwn(beforeProxy, "proxy_enabled"));
    await page.reload();
    await proxyCard.getByText("代理设置", { exact: true }).waitFor();
    assert.deepEqual(await request("config"), beforeProxy);
    const proxyURL = log.match(/BROWSER_PROXY_URL=(http:\/\/[^\s]+)/)?.[1];
    assert(proxyURL, "real proxy fixture missing");
    const proxyRequest = (target = "http://xvcf1.xboxlive.com/file", method = "GET") => new Promise((resolve, reject) => {
      const request = http.request(proxyURL, { method, path: target, headers: { Host: new URL(target).host } }, response => {
        let body = "";
        response.on("data", data => body += data);
        response.on("end", () => resolve({ status: response.statusCode, body }));
        response.on("error", reject);
      });
      request.on("error", reject);
      request.setTimeout(5000, () => request.destroy(new Error("proxy request timeout")));
      request.end();
    });
    assert.deepEqual(await proxyRequest(), { status: 200, body: "proxy-browser-fixture" });
    assert((await request("status")).proxy.downloaded_bytes > 0);
    const setup = page.locator(".console-setup");
    const parameters = page.locator(".connection-layout > .connection-modes");
    assert.equal(await parameters.count(), 1);
    assert.equal(await parameters.locator(".mode-card").count(), 2);
    assert.equal(await parameters.getByText(/服务状态|正在转发|本次运行累计下行|流量包含|停止服务前|备用 DNS 怎么填/).count(), 0);
    assert.equal(await parameters.locator("details").count(), 0);
    await proxyCard.getByText("不需要", { exact: true }).waitFor();
    await dnsCard.getByText("留空或同主 DNS", { exact: true }).waitFor();
    const connectionTabs = setup.getByRole("tablist", { name: "配置模式", exact: true });
    await connectionTabs.getByRole("tab", { name: "代理模式", exact: true }).focus();
    for (const [key, name] of [["ArrowRight", "DNS模式"], ["Home", "代理模式"], ["End", "DNS模式"], ["ArrowLeft", "代理模式"]]) {
      await page.keyboard.press(key);
      assert.equal(await connectionTabs.getByRole("tab", { name, exact: true }).getAttribute("aria-selected"), "true");
      assert(await proxyCard.isVisible() && await dnsCard.isVisible(), "guide tabs hid configuration parameters");
    }
    assert.equal(await page.locator("#connection-panel").getAttribute("aria-labelledby"), "connection-tab-proxy");
    assert.equal(await page.locator("main").getByRole("link", { name: "解析历史", exact: true }).count(), 0);
    assert.equal(await setup.getByRole("link", { name: "代理开关", exact: true }).count(), 0);
    await proxyCard.getByText("8082（TCP）", { exact: true }).waitFor();
    assert.equal(await dnsCard.getByText(/53（UDP|标准端口|端口/).count(), 0, "console DNS settings need only the address");
    assert.equal(await proxyCard.getByLabel("代理服务器", { exact: true }).inputValue(), "192.168.1.10");
    assert.equal(await dnsCard.getByLabel("主 DNS / 首选 DNS", { exact: true }).inputValue(), "192.168.1.10");
    for (const [card, label] of [[proxyCard, "复制代理地址"], [dnsCard, "复制 DNS 地址"]]) {
      const copy = card.getByRole("button", { name: label, exact: true });
      await copy.scrollIntoViewIfNeeded();
      const before = await card.evaluate(el => ({ height: el.getBoundingClientRect().height, factsTop: el.querySelector(".setup-facts").getBoundingClientRect().top }));
      await copy.click();
      await page.locator(".notice").getByText("地址已复制", { exact: true }).waitFor();
      assert.deepEqual(await card.evaluate(el => ({ height: el.getBoundingClientRect().height, factsTop: el.querySelector(".setup-facts").getBoundingClientRect().top })), before, "copy feedback shifted card content");
      assert.equal(await card.locator(".copy-feedback").count(), 0);
      await page.getByRole("button", { name: "关闭提示", exact: true }).click();
    }
    let advertised = ["192.168.1.10", "10.0.0.8"];
    const multipleAddresses = async route => {
      const response = await route.fetch();
      const status = await response.json();
      await route.fulfill({ response, json: { ...status, service_addresses: advertised, dns_address: advertised[0] || "", proxy: { ...status.proxy, address: advertised[0] || "" } } });
    };
    await page.route("**/api/v1/status", multipleAddresses);
    await page.reload();
    const proxyAddress = proxyCard.getByRole("combobox", { name: "代理服务器", exact: true });
    const dnsAddress = dnsCard.getByRole("combobox", { name: "主 DNS / 首选 DNS", exact: true });
    await proxyAddress.waitFor();
    assert.equal(await proxyAddress.innerText(), "192.168.1.10");
    assert.equal(await dnsAddress.innerText(), "192.168.1.10");
    await dnsAddress.click();
    await page.getByRole("option", { name: "10.0.0.8", exact: true }).click();
    assert.equal(await proxyAddress.innerText(), "10.0.0.8");
    await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined }));
    await proxyCard.getByRole("button", { name: "复制代理地址", exact: true }).click();
    await page.locator(".notice").getByText("地址已复制", { exact: true }).waitFor();
    await page.getByRole("button", { name: "关闭提示", exact: true }).click();
    assert.equal(await proxyAddress.innerText(), "10.0.0.8");
    await page.setViewportSize({ width: 320, height: 568 });
    await proxyAddress.click();
    await page.getByRole("option", { name: "192.168.1.10", exact: true }).waitFor();
    assert(!(await page.locator("body").innerText()).includes("127.0.0.1"));
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, "address selector overflow");
    await page.screenshot({ path: path.join(output, "address-options-mobile.png") });
    await page.keyboard.press("ArrowUp");
    await page.keyboard.press("Enter");
    assert.equal(await proxyAddress.innerText(), "192.168.1.10");
    assert.equal(await dnsAddress.innerText(), "192.168.1.10");
    advertised = [];
    await page.reload();
    await proxyCard.getByText(/尚未配置主机可用的服务地址/).waitFor();
    assert.equal(await page.locator(".connection-modes").getByRole("combobox").count(), 0);
    assert.equal(await page.locator(".connection-modes input").count(), 0);
    await page.unroute("**/api/v1/status", multipleAddresses);
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.reload();
    await proxyCard.getByLabel("代理服务器", { exact: true }).waitFor();
    await setup.getByRole("tab", { name: "Switch", exact: true }).click();
    await setup.getByText(/无需用户名或密码/).waitFor();
    await setup.getByRole("tab", { name: "Xbox", exact: true }).click();
    await setup.getByText(/Xbox 请使用 DNS模式/).waitFor();
    await connectionTabs.getByRole("tab", { name: "DNS模式", exact: true }).click();
    await setup.getByText(/备用 DNS 可留空，必须填写时使用相同地址/).waitFor();
    await setup.getByRole("tab", { name: "PlayStation", exact: true }).click();
    const modelTabs = setup.getByRole("tablist", { name: "主机型号", exact: true });
    assert.equal(await setup.locator(".guide-model select").count(), 0);
    assert.equal(await modelTabs.getByRole("tab", { name: "PS5", exact: true }).getAttribute("aria-selected"), "true");
    await modelTabs.getByRole("tab", { name: "PS4", exact: true }).click();
    await setup.getByText("设置 → 网络 → 设定互联网连接 → Wi-Fi / LAN → 自定义", { exact: true }).waitFor();
    for (const [key, name] of [["Home", "PS5"], ["End", "PS4"], ["ArrowRight", "PS5"], ["ArrowLeft", "PS4"]]) {
      await page.keyboard.press(key);
      const selected = modelTabs.getByRole("tab", { name, exact: true });
      assert.equal(await selected.getAttribute("aria-selected"), "true");
      assert.equal(await selected.getAttribute("tabindex"), "0");
      assert.equal(await page.locator("#model-guide-panel").getAttribute("aria-labelledby"), "model-tab-" + name);
      await setup.getByText(name === "PS5" ? "设置 → 网络 → 设置 → 设定互联网连接" : "设置 → 网络 → 设定互联网连接 → Wi-Fi / LAN → 自定义", { exact: true }).waitFor();
    }
    await setup.getByRole("tab", { name: "Xbox", exact: true }).click();
    assert.equal(await modelTabs.count(), 0);
    await setup.getByRole("tab", { name: "PlayStation", exact: true }).click();
    assert.equal(await modelTabs.getByRole("tab", { name: "PS4", exact: true }).getAttribute("aria-selected"), "true");
    await connectionTabs.getByRole("tab", { name: "代理模式", exact: true }).click();
    assert.equal(await modelTabs.getByRole("tab", { name: "PS4", exact: true }).getAttribute("aria-selected"), "true");
    await setup.getByText("设置 → 网络 → 设定互联网连接 → Wi-Fi / LAN → 自定义 → 代理设置", { exact: true }).waitFor();
    await modelTabs.getByRole("tab", { name: "PS5", exact: true }).click();
    assert(!(await page.locator("body").innerText()).includes("NAS"));
    assert(!(await page.locator(".network-board").innerText()).includes("自动选优"));
    assert((await page.locator(".aside-note").innerText()).includes("自动选优"));
    await page.emulateMedia({ reducedMotion: "no-preference" });
    assert(await page.evaluate(() => Array.from(document.querySelectorAll(".board-footer svg")).every(icon => getComputedStyle(icon).animationName === "none" && getComputedStyle(icon).transform === "none")), "overview status icons must remain still");
    const flowStart = await page.evaluate(() => Array.from(document.querySelectorAll(".route-diagram i"), line => ({ name: getComputedStyle(line, "::after").animationName, position: getComputedStyle(line, "::after").backgroundPositionX })));
    assert.equal(flowStart.length, 2);
    assert(flowStart.every(line => line.name === "route-flow"));
    await page.locator(".route-diagram").screenshot({ path: path.join(output, "route-flow-a.png") });
    await poll(async () => page.evaluate(start => Array.from(document.querySelectorAll(".route-diagram i")).every((line, index) => getComputedStyle(line, "::after").backgroundPositionX !== start[index].position), flowStart), "flow lines must visibly advance");
    await page.waitForTimeout(250);
    await page.locator(".route-diagram").screenshot({ path: path.join(output, "route-flow-b.png") });
    await page.emulateMedia({ reducedMotion: "reduce" });
    assert(await page.evaluate(() => Array.from(document.querySelectorAll(".route-diagram i")).every(line => getComputedStyle(line, "::after").animationName === "none")), "reduced motion must stop data flow");
    await setup.scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(output, "proxy-desktop.png") });
    const connectionLayouts = [];
    for (const [width, height] of [[1280, 800], [360, 640], [320, 568], [1024, 320]]) {
      await page.setViewportSize({ width, height });
      await proxyCard.getByRole("button", { name: "复制代理地址", exact: true }).scrollIntoViewIfNeeded();
      const copyLayoutBefore = await page.locator(".connection-layout").evaluate(el => ({ height: el.getBoundingClientRect().height, top: el.getBoundingClientRect().top }));
      await proxyCard.getByRole("button", { name: "复制代理地址", exact: true }).click();
      await page.locator(".notice").getByText("地址已复制", { exact: true }).waitFor();
      assert.deepEqual(await page.locator(".connection-layout").evaluate(el => ({ height: el.getBoundingClientRect().height, top: el.getBoundingClientRect().top })), copyLayoutBefore, "copy toast must not shift the page");
      assert(await proxyCard.evaluate(el => {
        const field = el.querySelector(".dns-copy").getBoundingClientRect(), button = el.querySelector(".copy-address").getBoundingClientRect();
        return button.left >= field.left && button.right <= field.right && button.top >= field.top && button.bottom <= field.bottom;
      }), "copy button must stay inside the field");
      await page.screenshot({ path: path.join(output, `copy-toast-${width}x${height}.png`) });
      await page.getByRole("button", { name: "关闭提示", exact: true }).click();
      await proxyCard.scrollIntoViewIfNeeded();
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `connection cards ${width} overflow`);
      await page.screenshot({ path: path.join(output, `modes-${width}x${height}.png`) });
      await setup.locator(".setup-guide").scrollIntoViewIfNeeded();
      await page.screenshot({ path: path.join(output, `configuration-${width}x${height}.png`) });
      const spacing = await page.evaluate(() => {
        const rect = selector => document.querySelector(selector).getBoundingClientRect();
        const board = rect(".network-board"), modes = rect(".connection-modes"), setup = rect(".console-setup"), platform = rect(".platform-summary");
        const middle = rect(".route-middle"), mark = rect(".route-middle .mark"), line = rect(".route-diagram i");
        const stacked = setup.top > modes.top + 1;
        return { connection: modes.top - board.bottom, platform: platform.top - Math.max(modes.bottom, setup.bottom),
          cardGap: stacked ? setup.top - modes.bottom : setup.left - modes.right, stacked,
          topOffset: Math.abs(setup.top - modes.top),
          diagramOffset: Math.abs((middle.left + middle.right - board.left - board.right) / 2), lineOffset: Math.abs((line.top + line.bottom - mark.top - mark.bottom) / 2) };
      });
      assert(spacing.connection >= 23.5 && spacing.platform >= 23.5 && Math.abs(spacing.cardGap - 24) < 1, JSON.stringify({ width, height, spacing }));
      assert.equal(spacing.stacked, width <= 760);
      if (!spacing.stacked) assert(spacing.topOffset <= 1, "parameter and guide cards must align at the top");
      assert(spacing.diagramOffset <= 1 && spacing.lineOffset <= 1, JSON.stringify({ width, height, spacing }));
      connectionLayouts.push({ width, height, ...spacing });
    }
    await page.setViewportSize({ width: 1280, height: 800 });
    assert.deepEqual(await proxyRequest(), { status: 200, body: "proxy-browser-fixture" });
    await page.getByRole("link", { name: "设置", exact: true }).click();
    assert.equal(await page.getByText(/每日预算|单节点读取上限/).count(), 0);
    const windowInput = page.getByLabel("单节点测速窗口（秒）", {
      exact: true,
    });
    await windowInput.fill("0");
    await save.click();
    await page.locator(".save-bar").getByRole("alert").waitFor();
    await windowInput.fill("2");
    await save.click();
    await poll(() => save.isDisabled(), "valid save did not recover");
    assert.deepEqual(await proxyRequest(), { status: 200, body: "proxy-browser-fixture" });
    const feedInput = page.getByLabel("规则表 URL", { exact: true });
    await feedInput.fill("https://rules.example.com/bad.json");
    await save.click();
    await poll(() => save.isDisabled(), "feed URL not saved");
    await page.getByRole("button", { name: "立即同步", exact: true }).click();
    await page
      .locator(".settings-section .error")
      .filter({ hasText: "继续使用上次可用规则" })
      .waitFor();
    assert.notEqual((await request("config")).rule_version, "bad");
    await feedInput.fill("https://rules.example.com/table.json");
    await save.click();
    await poll(() => save.isDisabled(), "valid URL not saved");
    await page.getByRole("button", { name: "立即同步", exact: true }).click();
    await page.getByText("规则表已同步", { exact: true }).waitFor();
    assert.equal((await request("config")).rule_version, "browser-v2");
    await page.getByRole("link", { name: "节点列表", exact: true }).click();
    await page.evaluate(() => Object.defineProperty(globalThis.crypto, "randomUUID", { value: undefined, configurable: true }));
    await page.getByRole("tab", { name: "PlayStation", exact: true }).click();
    await page.getByRole("button", { name: "添加节点", exact: true }).click();
    const addDialog = page.getByRole("dialog", { name: "添加节点", exact: true });
    await addDialog.waitFor();
    await page.keyboard.press("Escape");
    await addDialog.waitFor({ state: "hidden" });
    await page.getByRole("button", { name: "添加节点", exact: true }).click();
    await addDialog.waitFor();
    await page.mouse.click(4, 4);
    await addDialog.waitFor({ state: "hidden" });
    await page.getByRole("button", { name: "添加节点", exact: true }).click();
    const platformChoice = addDialog.getByRole("combobox", { name: "平台", exact: true });
    assert((await platformChoice.innerText()).includes("PlayStation"));
    await platformChoice.click();
    await page.getByRole("option", { name: "Xbox", exact: true }).click();
    await addDialog.getByLabel("测试下载 URL", { exact: true }).fill("http://xvcf2.xboxlive.com/large.bin");
    await addDialog.getByRole("button", { name: "保存", exact: true }).click();
    await addDialog.waitFor({ state: "hidden" });
    const saved = await request("config");
    assert.equal(saved.rules.length, 3);
    assert.equal(saved.rules[0].candidates[0], "1.1.1.1");
    assert.equal(saved.rules[2].platform, "Xbox");
    await page.getByRole("tab", { name: "Xbox", exact: true }).click();
    await page.getByText("xvcf2.xboxlive.com", { exact: true }).waitFor();
    const capturedURL = "http://recorded.xboxlive.com/releases/game%20sample.bin?build=42&ticket=demo%2Bvalue";
    const supplementURL = "http://xvcf2.xboxlive.com/range-file.bin?ticket=demo&part=2";
    assert.deepEqual(await proxyRequest(capturedURL), { status: 200, body: "proxy-browser-fixture" });
    let capturePage = await request("connections");
    assert.equal(capturePage.page_size, 10);
    assert(capturePage.items.some(record => record.address === capturedURL && record.client === "127.0.0.1" && record.method === "GET"));
    await page.getByRole("link", { name: "连接记录", exact: true }).click();
    const connectionRecords = page.getByRole("region", { name: "连接记录", exact: true });
    const capturedRow = connectionRecords.locator("tbody tr").filter({ hasText: capturedURL });
    await capturedRow.waitFor();
    await capturedRow.getByRole("button", { name: "添加节点", exact: true }).click();
    assert.equal(await addDialog.getByLabel("测试下载 URL", { exact: true }).inputValue(), capturedURL);
    await addDialog.getByRole("combobox", { name: "平台", exact: true }).click();
    await page.getByRole("option", { name: "Xbox", exact: true }).click();
    await addDialog.getByRole("button", { name: "保存", exact: true }).click();
    await addDialog.waitFor({ state: "hidden" });
    assert((await request("config")).rules.some(rule => rule.domain === "recorded.xboxlive.com" && rule.url === capturedURL));
    await capturedRow.getByRole("button", { name: "已收录", exact: true }).waitFor();
    const advanced = await request("config");
    const existingRule = advanced.rules.find(rule => rule.domain === "xvcf2.xboxlive.com");
    existingRule.enabled = false;
    existingRule.expected_prefix = "demo";
    existingRule.min_bytes = 1024;
    existingRule.candidates = ["1.1.1.1", "8.8.8.8"];
    await request("config", advanced, "PUT");
    assert.deepEqual(await proxyRequest(supplementURL), { status: 200, body: "proxy-browser-fixture" });
    await connectionRecords.getByRole("button", { name: "刷新最新", exact: true }).click();
    const supplementRow = connectionRecords.locator("tbody tr").filter({ hasText: supplementURL });
    await supplementRow.getByRole("button", { name: "补充节点", exact: true }).click();
    const supplementDialog = page.getByRole("dialog", { name: "补充节点", exact: true });
    assert(await supplementDialog.getByRole("combobox", { name: "平台", exact: true }).isDisabled());
    await supplementDialog.getByRole("button", { name: "更新", exact: true }).click();
    await supplementDialog.waitFor({ state: "hidden" });
    const supplemented = await request("config");
    assert.deepEqual(supplemented, { ...advanced, rules: advanced.rules.map(rule => rule.domain === existingRule.domain ? { ...rule, url: supplementURL } : rule) });
    const authority = "new.cdn.nintendo.net:443";
    await new Promise((resolve, reject) => {
      const req = http.request(proxyURL, { method: "CONNECT", path: authority, headers: { Host: authority } });
      req.on("connect", (response, socket) => { socket.destroy(); response.statusCode === 200 ? resolve() : reject(new Error("CONNECT rejected")); });
      req.on("error", reject);
      req.setTimeout(5000, () => req.destroy(new Error("CONNECT timeout")));
      req.end();
    });
    await connectionRecords.getByRole("button", { name: "刷新最新", exact: true }).click();
    const tunnelRow = connectionRecords.locator("tbody tr").filter({ hasText: authority });
    await tunnelRow.getByText("HTTPS 隧道，仅域名和端口", { exact: true }).waitFor();
    await tunnelRow.getByRole("button", { name: "补充下载地址", exact: true }).click();
    assert.equal(await addDialog.getByLabel("测试下载 URL", { exact: true }).inputValue(), "");
    await addDialog.getByRole("button", { name: "保存", exact: true }).click();
    assert.equal((await request("config")).rules.some(rule => rule.domain === "new.cdn.nintendo.net"), false);
    await addDialog.getByRole("combobox", { name: "平台", exact: true }).click();
    await page.getByRole("option", { name: "Switch", exact: true }).click();
    await addDialog.getByLabel("测试下载 URL", { exact: true }).fill("https://wrong.cdn.nintendo.net/game.bin");
    await addDialog.getByRole("button", { name: "保存", exact: true }).click();
    await addDialog.getByRole("alert").getByText("请填写记录中相同域名的下载 URL", { exact: true }).waitFor();
    await addDialog.getByLabel("测试下载 URL", { exact: true }).fill("https://new.cdn.nintendo.net/samples/game.bin?ticket=demo");
    await page.screenshot({ path: path.join(output, "connections-https-complete.png") });
    await addDialog.getByRole("button", { name: "保存", exact: true }).click();
    await addDialog.waitFor({ state: "hidden" });
    assert((await request("config")).rules.some(rule => rule.domain === "new.cdn.nintendo.net" && rule.platform === "Switch" && rule.url.includes("/samples/game.bin")));
    capturePage = await request("connections");
    assert(capturePage.items.some(record => record.method === "CONNECT" && record.address === authority && !record.address.includes("/samples")));
    for (let i = 0; i < 12; i++) await proxyRequest("http://recorded.xboxlive.com/chunk-" + i + ".bin?part=" + i);
    await connectionRecords.getByRole("button", { name: "刷新最新", exact: true }).click();
    await connectionRecords.getByRole("button", { name: "下一页", exact: true }).click();
    await connectionRecords.getByText(/第 2 \/ /).waitFor();
    await page.screenshot({ path: path.join(output, "connections-desktop.png") });
    await page.getByRole("link", { name: "设置", exact: true }).click();
    await save.waitFor();
    for (const [width, height] of [
      [1280, 800],
      [1024, 320],
      [360, 640],
      [360, 480],
      [320, 568],
    ]) {
      await page.setViewportSize({ width, height });
      for (const position of ["top", "bottom"]) {
        await page
          .locator(".workspace")
          .evaluate(
            (el, pos) => el.scrollTo(0, pos === "bottom" ? el.scrollHeight : 0),
            position,
          );
        const boxes = await page.evaluate(() => {
          const rect = (s) => {
            const r = document.querySelector(s).getBoundingClientRect();
            return { x: r.x, y: r.y, right: r.right, bottom: r.bottom };
          };
          return {
            save: rect(".save-bar button"),
            dock: rect(".settings-actions"),
            main: rect(".workspace"),
            status: rect(".aside-note"),
            overflow: document.documentElement.scrollWidth > innerWidth,
          };
        });
        assert(!boxes.overflow, JSON.stringify({ width, height, boxes }));
        assert(
          boxes.save.x >= 0 &&
            boxes.save.y >= 0 &&
            boxes.save.right <= width &&
            boxes.save.bottom <= height,
          JSON.stringify({ width, height, boxes }),
        );
        assert(
          boxes.main.bottom <= boxes.dock.y + 1,
          JSON.stringify({ width, height, boxes }),
        );
        if (width <= 760)
          assert(
            boxes.dock.bottom <= boxes.status.y + 1,
            JSON.stringify({ width, height, boxes }),
          );
      }
      await page.screenshot({
        path: path.join(output, `settings-${width}x${height}.png`),
      });
      for (const [name, url] of [
        ["网络总览", "/"],
        ["节点列表", "/platforms"],
        ["连接记录", "/connections"],
        ["解析历史", "/connection"],
        ["测速记录", "/history"],
        ["运行日志", "/events"],
      ]) {
        await page.getByRole("link", { name, exact: true }).click();
        await page.waitForTimeout(100);
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          ),
          false,
          `${name} ${width} overflow`,
        );
      }
      await page.getByRole("link", { name: "设置", exact: true }).click();
      await save.waitFor();
    }
    assert.deepEqual(errors, []);
    const report = {
      passed: true,
      backend: "real Go/SQLite/DNS/HTTP fixtures",
      connection_layouts: connectionLayouts,
      checks: [
        "automatic selection",
        "API pin/default regression reflected in node list",
        "platform tab arrow/Home/End keyboard interaction",
        "sync failure shows pending before actual default applies",
        "restore default and retest",
        "health failure fallback and recovery",
        "actual DNS check",
        "record pagination and server filter",
        "daily summary API retained",
        "sidebar footer shows the running backend application version",
        "proxy available at startup, reload, actual forwarded bytes and preserved after settings save",
        "one parameter card with both modes and brief benefits, no telemetry/footer descriptions, guide at right, keyboard and mobile stacking",
        "LAN address first item, shared multi-address selector, keyboard/mobile, clipboard fallback and empty addresses",
        "in-field copy button, floating feedback without layout shift, DNS address without a port row",
        "PS5/PS4 model tabs update guide pages, keyboard/panel semantics and selection retention",
        "centered route diagram, static probe icon, moving connection segments and reduced motion",
        "real validation failure and save retry",
        "bad feed preserves last good / valid feed applies",
        "node modal with Radix platform selection, filtered defaults and field preservation",
        "captured full HTTP URLs to node creation and existing sample supplement with all other fields preserved",
        "CONNECT authority only, manual download path required, no guessed HTTPS URL",
        "shared node dialog cancel/reopen, connection pagination, seven pages and five viewport sizes; fixed save dock",
      ],
    };
    fs.writeFileSync(
      path.join(output, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify(report));
  } finally {
    if (browser) await browser.close();
    if (base) {
      try {
        await fetch(base + "/fixture/stop", { method: "POST" });
      } catch {}
    }
    let exitTimer;
    const code = await Promise.race([
      exited,
      new Promise((r) => {
        exitTimer = setTimeout(() => r(null), 10000);
      }),
    ]);
    clearTimeout(exitTimer);
    if (code === null) child.kill();
    fs.writeFileSync(path.join(output, "fixture.log"), log);
    if (code !== 0)
      throw new Error("fixture failed: " + code + "\n" + log.slice(-3000));
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
  child.kill();
});
