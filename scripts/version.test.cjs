const assert = require("node:assert/strict");
const { test } = require("node:test");
const {
  mkdtempSync,
  mkdirSync,
  copyFileSync,
  readFileSync,
  writeFileSync,
  rmSync,
} = require("node:fs");
const { tmpdir } = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

test("image publishing only declares latest and the complete application version", () => {
  const workflow = readFileSync(
    path.resolve(__dirname, "../.github/workflows/docker.yml"),
    "utf8",
  );
  const tags = workflow.match(/          tags: \|\r?\n([\s\S]*?)          labels:/)[1];
  assert.deepEqual(
    tags.trim().split(/\r?\n/).map((line) => line.trim().split(",").slice(0, 2).join(",")),
    [
      "type=raw,value=${{ needs.test.outputs.version }}",
      "type=raw,value=latest",
    ],
  );
  assert.match(workflow, /flavor: latest=false/);
  assert.match(
    workflow,
    /name: Publish amd64 and arm64\r?\n    if: github.event_name != 'pull_request' && \(github.ref == 'refs\/heads\/main' \|\| startsWith\(github.ref, 'refs\/tags\/v'\)\)/,
  );
});

test("version synchronization and release checks use the real project files", (t) => {
  const root = path.resolve(__dirname, "..");
  const fixture = mkdtempSync(path.join(tmpdir(), "dns-assistant-version-"));
  t.after(() => rmSync(fixture, { recursive: true, force: true }));
  const files = [
    "scripts/version.cjs",
    "internal/app/VERSION",
    "package.json",
    "web/package.json",
    "web/package-lock.json",
    "README.md",
    ".env.example",
    "website/index.html",
  ];
  for (const file of files) {
    mkdirSync(path.dirname(path.join(fixture, file)), { recursive: true });
    copyFileSync(path.join(root, file), path.join(fixture, file));
  }
  const run = (...args) =>
    spawnSync(
      process.execPath,
      [path.join(fixture, "scripts/version.cjs"), ...args],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          RELEASE_REF: "refs/tags/v12.34.56",
          GITHUB_OUTPUT: path.join(fixture, "output"),
        },
      },
    );

  writeFileSync(path.join(fixture, "internal/app/VERSION"), "12.34.56\n");
  let result = run("--write");
  assert.equal(result.status, 0, result.stderr);
  assert.notEqual(run().status, 0, "missing release notes must fail");
  mkdirSync(path.join(fixture, "docs/releases"), { recursive: true });
  writeFileSync(
    path.join(fixture, "docs/releases/v12.34.56.md"),
    "Release notes\n",
  );
  result = run();
  assert.equal(result.status, 0, result.stderr);
  assert.equal(
    readFileSync(path.join(fixture, "output"), "utf8"),
    "version=12.34.56\n",
  );

  const synchronized = files.map((file) =>
    readFileSync(path.join(fixture, file), "utf8"),
  );
  assert.equal(run("--write").status, 0);
  assert.deepEqual(
    files.map((file) => readFileSync(path.join(fixture, file), "utf8")),
    synchronized,
  );

  for (const [file, before, after] of [
    ["package.json", '"version": "12.34.56"', '"version": "1.0.0"'],
    ["web/package.json", '"version": "12.34.56"', '"version": "1.0.0"'],
    ["web/package-lock.json", '"version": "12.34.56"', '"version": "1.0.0"'],
    [
      "web/package-lock.json",
      '      "version": "12.34.56"',
      '      "version": "1.0.0"',
    ],
    ["README.md", "**v12.34.56**", "**v1.0.0**"],
    [".env.example", "dns-assistant:12.34.56", "dns-assistant:latest"],
    [
      ".env.example",
      "ghcr.nju.edu.cn/leoskey/dns-assistant:12.34.56",
      "ghcr.nju.edu.cn/leoskey/dns-assistant:latest",
    ],
    ["website/index.html", ">v12.34.56</span", ">v1.0.0</span"],
    ["website/index.html", 'class="version"', 'class="old-version"'],
    [
      "website/index.html",
      "dns-assistant-12.34.56-windows",
      "dns-assistant-1.0.0-windows",
    ],
  ]) {
    const target = path.join(fixture, file);
    const original = readFileSync(target, "utf8");
    assert(original.includes(before), `test field missing: ${file} ${before}`);
    writeFileSync(target, original.replace(before, after));
    assert.notEqual(run().status, 0, `version drift must fail: ${file}`);
    assert.equal(
      readFileSync(target, "utf8"),
      original.replace(before, after),
      "check must not write",
    );
    writeFileSync(target, original);
  }

  writeFileSync(path.join(fixture, "internal/app/VERSION"), "12.34.57\n");
  assert.equal(run("--write").status, 0);
  writeFileSync(
    path.join(fixture, "docs/releases/v12.34.57.md"),
    "Release notes\n",
  );
  assert.notEqual(run().status, 0, "mismatched Git tag must fail");
  const beforeInvalid = readFileSync(
    path.join(fixture, "package.json"),
    "utf8",
  );
  for (const invalid of ["v1.2.3", "01.2.3", "1.2", "1.2.3-beta"]) {
    writeFileSync(path.join(fixture, "internal/app/VERSION"), invalid);
    assert.notEqual(run("--write").status, 0, "invalid version must fail");
    assert.equal(
      readFileSync(path.join(fixture, "package.json"), "utf8"),
      beforeInvalid,
    );
  }
});
