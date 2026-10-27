// Opt-in end-to-end test against the actual Go/SQLite/DNS fixture.
// Set APP_TEST_BINARY, PLAYWRIGHT_MODULE, CHROME_PATH and BROWSER_OUTPUT_DIR.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const { spawn } = require("node:child_process");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const binary = process.env.APP_TEST_BINARY;
if (!binary) throw new Error("APP_TEST_BINARY must point to go test -c output");
const output =
  process.env.BROWSER_OUTPUT_DIR || path.resolve("work/browser-check");
fs.mkdirSync(output, { recursive: true });
const child = spawn(binary, ["-test.run", "^TestBrowserFixture$", "-test.v"], {
  env: {
    ...process.env,
    DNS_ASSISTANT_BROWSER_FIXTURE: "1",
    NAS_IP: "192.168.1.10",
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
    const read = async (route) => {
      const r = await fetch(base + "/api/v1/" + route);
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
      const s = await read("status");
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
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(base + "/platforms");
    const row = () => page.locator(".platform-table tbody > tr").first();
    await poll(async () => {
      const s = await read("status");
      return (
        s.selections.filter(active).length === 2 &&
        s.version === s.applied_version &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "retest not applied");
    await row().getByText("优选使用中", { exact: true }).waitFor();
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
    await row().getByRole("button", { name: "详情", exact: true }).click();
    await page.getByText("高级操作：固定节点", { exact: true }).click();
    await page.getByLabel("公网 IPv4", { exact: true }).fill("1.1.1.1");
    await page.getByRole("button", { name: "验证并固定", exact: true }).click();
    await poll(async () => {
      const s = await read("status");
      return (
        s.selections.some((v) => v.fixed && active(v)) &&
        s.version === s.applied_version
      );
    }, "verified pin did not apply");
    await row().getByText("固定节点", { exact: true }).waitFor();
    await row().getByRole("button", { name: "详情", exact: true }).click();
    await fixture("sync-off");
    await row().getByRole("button", { name: "恢复默认", exact: true }).click();
    await row().getByText("策略同步中", { exact: true }).waitFor();
    assert.match(await row().innerText(), /等待同步/);
    await fixture("sync-on");
    await row().getByText("默认使用中", { exact: true }).waitFor();
    let status = await read("status");
    const id = status.rules[0].id;
    assert(!status.selections.find((s) => s.rule_id === id && active(s)));
    await row().getByRole("button", { name: "立即复测", exact: true }).click();
    await poll(async () => {
      const s = await read("status");
      return (
        s.selections.filter(active).length === 2 &&
        s.version === s.applied_version &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "retest not applied");
    await row().getByText("优选使用中", { exact: true }).waitFor();
    await fixture("fail-health");
    await poll(
      async () =>
        !(await read("status")).selections.some(
          (s) => s.rule_id === id && active(s),
        ),
      "health failure did not restore default",
    );
    await poll(
      async () =>
        (await read("status")).tasks.some(
          (t) => t.rule_id === id && t.state === "error",
        ),
      "failed recovery did not settle",
    );
    await fixture("recover");
    await row().getByRole("button", { name: "立即复测", exact: true }).click();
    await poll(async () => {
      const s = await read("status");
      return (
        s.selections.filter(active).length === 2 &&
        s.version === s.applied_version &&
        !s.tasks.some((t) => t.state === "running" || t.state === "queued")
      );
    }, "retest not applied");
    await row().getByText("优选使用中", { exact: true }).waitFor();
    await page.screenshot({ path: path.join(output, "platforms-desktop.png") });
    await page.getByRole("link", { name: "接入检查", exact: true }).click();
    assert.equal(await page.locator("#query-type").count(), 0);
    await page.getByRole("button", { name: "检查解析", exact: true }).click();
    await page
      .locator(".check-result")
      .getByText(/NOERROR/)
      .waitFor();
    assert.match(await page.locator(".check-result").innerText(), /1\.1\.1\.1/);
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
    await page
      .getByRole("region", { name: "每日汇总" })
      .locator("tbody tr")
      .first()
      .waitFor();
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
    const appVersion = (await read("status")).app_version;
    assert(appVersion, "application version missing from status");
    assert(
      (await page.getByTestId("app-version").innerText()).includes(
        `v${appVersion}`,
      ),
      "sidebar footer must display the running backend version",
    );
    assert(await save.isDisabled());
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
    assert.notEqual((await read("config")).rule_version, "bad");
    await feedInput.fill("https://rules.example.com/table.json");
    await save.click();
    await poll(() => save.isDisabled(), "valid URL not saved");
    await page.getByRole("button", { name: "立即同步", exact: true }).click();
    await page.getByText("规则表已同步", { exact: true }).waitFor();
    assert.equal((await read("config")).rule_version, "browser-v2");
    await page
      .getByRole("button", { name: "添加下载规则", exact: true })
      .click();
    await page
      .getByLabel("测试下载地址", { exact: true })
      .fill("http://xvcf2.xboxlive.com/large.bin");
    await save.click();
    await poll(() => save.isDisabled(), "new rule save failed");
    const saved = await read("config");
    assert.equal(saved.rules.length, 3);
    assert.equal(saved.rules[0].candidates[0], "1.1.1.1");
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
        ["平台选优", "/platforms"],
        ["接入检查", "/connection"],
        ["测速记录", "/history"],
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
        "verified advanced pin",
        "platform tab arrow/Home/End keyboard interaction",
        "sync failure shows pending before actual default applies",
        "restore default and retest",
        "health failure fallback and recovery",
        "actual DNS check",
        "record pagination and server filter",
        "daily summary",
        "settings shows the running backend application version",
        "real validation failure and save retry",
        "bad feed preserves last good / valid feed applies",
        "simple rule add and advanced field preservation",
        "five pages and five viewport sizes; fixed save dock",
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
