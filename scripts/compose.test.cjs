const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { test } = require("node:test");

test("README uses the shipped Compose configuration without required variables", () => {
  const root = path.resolve(__dirname, "..");
  const compose = fs.readFileSync(path.join(root, "compose.yaml"), "utf8");
  const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");
  assert.equal(readme.match(/```yaml\r?\n([\s\S]*?)```/)[1], compose);
  assert(!compose.includes("${"));
  assert(!compose.includes("SERVICE_TOKEN"));
  assert.match(compose, /\n    volumes:\r?\n      - data:\/data\r?\n    read_only:/);
  assert.match(compose, /\nvolumes:\r?\n  data:\r?\n$/);
});

test("Compose launcher preserves arguments, environment and exit status", (t) => {
  const fixture = fs.mkdtempSync(path.join(os.tmpdir(), "dns compose-"));
  t.after(() => fs.rmSync(fixture, { recursive: true, force: true }));
  const bin = path.join(fixture, "bin");
  fs.mkdirSync(bin);
  fs.mkdirSync(path.join(fixture, "deploy"));
  fs.copyFileSync(path.join(__dirname, "../deploy/compose.sh"), path.join(fixture, "deploy/compose.sh"));
  fs.writeFileSync(path.join(bin, "docker"), '#!/bin/sh\nprintf "%s\\n" "$@" "$DOCKER_HOST" > "$CAPTURE"\nexit "$TEST_EXIT"\n', { mode: 0o755 });
  const capture = path.join(fixture, "capture");
  const shell = process.platform === "win32" ? "C:/Program Files/Git/bin/sh.exe" : "/bin/sh";
  for (const exit of [0, 7]) {
    const args = ["--env-file", "custom file.env", "up", "-d", "--wait"];
    const result = spawnSync(shell, [path.join(fixture, "deploy/compose.sh"), ...args], {
      cwd: bin,
      encoding: "utf8",
      timeout: 10000,
      env: { ...process.env, PATH: bin + path.delimiter + process.env.PATH, CAPTURE: capture, TEST_EXIT: String(exit), DOCKER_HOST: "ssh://example.invalid" },
    });
    assert.equal(result.status, exit, result.stderr);
    assert.deepEqual(fs.readFileSync(capture, "utf8").trim().split(/\r?\n/), ["compose", ...args, "ssh://example.invalid"]);
  }
});
