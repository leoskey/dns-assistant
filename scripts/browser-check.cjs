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
    const proxyToggle = page.getByLabel("启用下载代理", { exact: true });
    assert(!(await proxyToggle.isChecked()));
    await proxyToggle.check();
    await save.click();
    await poll(() => save.isDisabled(), "proxy setting was not saved");
    await page.reload();
    await proxyToggle.waitFor();
    assert(await proxyToggle.isChecked());
    const proxyURL = log.match(/BROWSER_PROXY_URL=(http:\/\/[^\s]+)/)?.[1];
    assert(proxyURL, "real proxy fixture missing");
    const proxyRequest = () => new Promise((resolve, reject) => {
      const request = http.request(proxyURL, { path: "http://xvcf1.xboxlive.com/file", headers: { Host: "xvcf1.xboxlive.com" } }, response => {
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
    await page.getByRole("link", { name: "网络总览", exact: true }).click();
    await page.getByLabel("接入方式", { exact: true }).selectOption("proxy");
    const setup = page.locator(".console-setup");
    assert.equal(await setup.getByRole("link", { name: "代理设置", exact: true }).getAttribute("href"), "/settings");
    await setup.getByText("8082（TCP）", { exact: true }).waitFor();
    await setup.getByText("下载代理已启用", { exact: true }).waitFor();
    assert.equal(await setup.getByLabel("代理服务器", { exact: true }).inputValue(), "192.168.1.10");
    await setup.getByRole("button", { name: "复制代理地址", exact: true }).click();
    await poll(async () => (await setup.locator(".copy-feedback").innerText()).includes("复制"), "copy feedback missing");
    await setup.getByRole("tab", { name: "Switch", exact: true }).click();
    await setup.getByText(/无需用户名或密码/).waitFor();
    await setup.getByRole("tab", { name: "Xbox", exact: true }).click();
    await setup.getByText(/Xbox 请使用 DNS 优选/).waitFor();
    await setup.getByRole("tab", { name: "PlayStation", exact: true }).click();
    assert(!(await page.locator("body").innerText()).includes("NAS"));
    await setup.scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(output, "proxy-desktop.png") });
    for (const [width, height] of [[360, 640], [320, 568], [1024, 320]]) {
      await page.setViewportSize({ width, height });
      await setup.locator(".setup-config").scrollIntoViewIfNeeded();
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `proxy guide ${width} overflow`);
      await page.screenshot({ path: path.join(output, `proxy-${width}x${height}.png`) });
      await setup.locator(".setup-guide").scrollIntoViewIfNeeded();
      await page.screenshot({ path: path.join(output, `proxy-guide-${width}x${height}.png`) });
    }
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.getByRole("link", { name: "设置", exact: true }).click();
    await proxyToggle.uncheck();
    await save.click();
    await poll(() => save.isDisabled(), "proxy disable was not saved");
    assert.equal((await proxyRequest()).status, 503);
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
        "proxy enable/reload, actual forwarded bytes, platform guides/copy and disable",
        "real validation failure and save retry",
        "bad feed preserves last good / valid feed applies",
        "node modal with Radix platform selection, filtered defaults and field preservation",
        "six pages and five viewport sizes; fixed save dock",
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
