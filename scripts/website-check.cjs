// Run against the website with the same Playwright setup as browser-check.cjs.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

(async () => {
  const output = path.resolve(
    process.env.BROWSER_OUTPUT_DIR || "artifacts/website-check",
  );
  fs.mkdirSync(output, { recursive: true });
  const browser = await chromium.launch({
    headless: true,
    ...(process.env.CHROME_PATH
      ? { executablePath: process.env.CHROME_PATH }
      : {}),
  });
  try {
    const page = await browser.newPage({
      viewport: { width: 1440, height: 1000 },
      reducedMotion: "reduce",
    });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("response", (response) => {
      if (response.status() >= 400)
        errors.push(`${response.status()} ${response.url()}`);
    });
    assert(
      (
        await page.goto(process.env.WEBSITE_URL || "http://127.0.0.1:5174")
      ).ok(),
    );
    await page.getByRole("heading", { level: 1 }).waitFor();
    assert.equal(await page.title(), "DNS Assistant — 为主机下载，选好节点");
    assert.equal(await page.getByRole("heading", { level: 1 }).count(), 1);
    assert(
      !/\bNAS\b/i.test(await page.locator("body").textContent()),
      "website must not advertise NAS support",
    );
    for (const meta of await page
      .locator('meta[name="description"], meta[property="og:description"]')
      .all())
      assert(!/\bNAS\b/i.test(await meta.getAttribute("content")));
    await page.keyboard.press("Tab");
    assert.equal(await page.locator(":focus").textContent(), "跳到主要内容");
    assert.equal(
      await page
        .locator(
          "[data-design], [data-hero], .instrument, .atlas, .hero-type, #github, .ps-icon, .switch-icon",
        )
        .count(),
      0,
    );
    assert.equal(
      await page
        .locator(
          ".platform-names svg, .faq-section, .preview-caption, .scope-note",
        )
        .count(),
      0,
    );
    assert(
      await page
        .locator(".hero-art")
        .evaluate((image) => image.complete && image.naturalWidth > 0),
    );
    const platforms = page.getByRole("group", { name: "选择主机查看接入指引" });
    for (const [name, heading, fragment] of [
      ["Xbox", "在 Xbox 上设置 DNS", "高级设置 → DNS 设置"],
      ["Switch", "在 Nintendo Switch 上设置 DNS", "更改设置"],
      ["PlayStation", "在 PS5 上设置 DNS", "设定互联网连接"],
    ]) {
      const button = platforms.getByRole("button", { name, exact: true });
      await button.focus();
      await page.keyboard.press("Enter");
      assert.equal(await button.getAttribute("aria-pressed"), "true");
      assert.equal(await platforms.locator('[aria-pressed="true"]').count(), 1);
      assert.equal(await page.locator("#guide-title").textContent(), heading);
      assert(
        (await page.locator("#guide-path").textContent()).includes(fragment),
      );
    }
    await page.evaluate(() => {
      window.copiedCommand = "";
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: {
          writeText: async (text) => {
            window.copiedCommand = text;
          },
        },
      });
    });
    await page.getByRole("button", { name: "复制命令" }).click();
    assert.equal(
      await page.evaluate(() => window.copiedCommand),
      await page.locator("#install-command").textContent(),
    );
    assert.equal(await page.getByRole("status").textContent(), "已复制");
    await page.evaluate(() => {
      navigator.clipboard.writeText = async () => {
        throw new Error("denied");
      };
    });
    await page.getByRole("button", { name: "复制命令" }).click();
    assert(
      (await page.getByRole("status").textContent()).includes("命令已选中"),
    );
    assert.equal(
      await page.evaluate(() => getSelection().toString()),
      await page.locator("#install-command").textContent(),
    );
    const invalidLinks = await page
      .locator('a[href^="#"]')
      .evaluateAll((links) =>
        links
          .filter(
            (link) =>
              link.hash &&
              !document.getElementById(decodeURIComponent(link.hash.slice(1))),
          )
          .map((link) => link.href),
      );
    assert.deepEqual(invalidLinks, []);
    await page.reload();
    for (const width of [1440, 1024, 768, 390, 360]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate(() => scrollTo(0, 0));
      await page.screenshot({
        path: path.join(output, `website-b-${width}.png`),
        fullPage: true,
      });
      if (width === 1440 || width === 360)
        await page.screenshot({
          path: path.join(output, `website-b-${width}-first-screen.png`),
        });
      assert(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        `overflow at ${width}px`,
      );
      const sizes = await page
        .locator("body *")
        .evaluateAll((elements) => [
          ...new Set(
            elements
              .filter(
                (element) =>
                  element.getClientRects().length > 0 &&
                  [...element.childNodes].some(
                    (node) =>
                      node.nodeType === Node.TEXT_NODE &&
                      node.textContent.trim(),
                  ),
              )
              .map((element) => getComputedStyle(element).fontSize),
          ),
        ]);
      assert(sizes.length <= 4, `${width}px type sizes: ${sizes}`);
      assert(sizes.every((size) => parseFloat(size) >= 12));
    }
    const contrasts = await page.evaluate(() => {
      const luminance = (color) =>
        color
          .match(/[\d.]+/g)
          .slice(0, 3)
          .map(Number)
          .map((value) => {
            const n = value / 255;
            return n <= 0.04045 ? n / 12.92 : ((n + 0.055) / 1.055) ** 2.4;
          })
          .reduce(
            (total, value, index) =>
              total + value * [0.2126, 0.7152, 0.0722][index],
            0,
          );
      const ratio = (foreground, background) => {
        const pair = [luminance(foreground), luminance(background)].sort(
          (a, b) => b - a,
        );
        return (pair[0] + 0.05) / (pair[1] + 0.05);
      };
      const button = getComputedStyle(
        document.querySelector(".hero .button.primary"),
      );
      const root = getComputedStyle(document.documentElement);
      const navigation = getComputedStyle(
        document.querySelector(".header nav"),
      );
      return {
        button: ratio(button.color, button.backgroundColor),
        navigation: ratio(navigation.color, root.backgroundColor),
      };
    });
    assert(contrasts.button >= 4.5, `button contrast ${contrasts.button}`);
    assert(
      contrasts.navigation >= 4.5,
      `navigation contrast ${contrasts.navigation}`,
    );
    assert.deepEqual(errors, []);
    console.log(
      "Website checks passed: B direction, 5 widths, artwork, keyboard, platforms, copy/fallback, anchors, logo/old-layout removal, contrast, type sizes, browser errors.",
    );
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
