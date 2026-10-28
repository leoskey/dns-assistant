const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const fixture = fs.mkdtempSync(path.join(os.tmpdir(), "dns compose test-"));
const bin = path.join(fixture, "bin");
fs.mkdirSync(bin);
fs.mkdirSync(path.join(fixture, "deploy"));
fs.copyFileSync(
  path.join(__dirname, "../deploy/compose.sh"),
  path.join(fixture, "deploy/compose.sh"),
);
fs.writeFileSync(
  path.join(bin, "docker"),
  `#!/bin/sh
if [ "$1" = compose ] && [ "\${2:-}" = -f ] && [ "\${3:-}" = - ]; then
    [ "\${TEST_CONFIG_ERROR:-0}" = 0 ] || exit "\${TEST_CONFIG_ERROR}"
    printf 'SERVER_IP=%s\\n' "\${TEST_CONFIG_IP:-}"
elif [ "$1" = context ]; then
    printf '%s\\n' "\${TEST_ENDPOINT:-unix:///var/run/docker.sock}"
else
    printf '%s\\n' "$SERVER_IP" "$@" > "$CAPTURE"
    exit "\${TEST_EXIT:-0}"
fi
`,
  { mode: 0o755 },
);
fs.writeFileSync(
  path.join(bin, "ip"),
  `#!/bin/sh
if [ "$1" = -4 ]; then
    [ "\${TEST_NO_ROUTE:-}" != 1 ] || exit 1
    printf '%s\\n' "\${TEST_ROUTE:-192.0.2.1 via 192.168.50.1 dev eth0 src 192.168.50.20}"
elif [ "$2" = link ]; then
    printf '%s\\n' "\${TEST_LINK:-2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500}"
else
    printf '%s\\n' "\${TEST_ADDRESS:-2: eth0 inet 192.168.50.20/24 brd 192.168.50.255 scope global eth0}"
fi
`,
  { mode: 0o755 },
);

const shell =
  process.platform === "win32" ? "C:/Program Files/Git/bin/sh.exe" : "/bin/sh";
let checks = 0;
function run(env = {}, args = ["up", "-d", "--wait"]) {
  const capture = path.join(fixture, "capture");
  fs.rmSync(capture, { force: true });
  const result = spawnSync(shell, ["deploy/compose.sh", ...args], {
    cwd: fixture,
    encoding: "utf8",
    timeout: 30000,
    env: {
      ...process.env,
      PATH: bin + path.delimiter + process.env.PATH,
      SERVER_IP: "",
      DOCKER_HOST: "",
      DOCKER_CONTEXT: "",
      TEST_CONFIG_IP: "",
      CAPTURE: capture,
      ...env,
    },
  });
  if (result.error) {
    result.error.message += result.stderr;
    throw result.error;
  }
  checks++;
  return {
    ...result,
    forwarded: fs.existsSync(capture)
      ? fs.readFileSync(capture, "utf8").trim().split("\n")
      : null,
  };
}

try {
  const automatic = run();
  assert.equal(automatic.status, 0, automatic.stderr);
  assert.deepEqual(automatic.forwarded, [
    "192.168.50.20",
    "compose",
    "up",
    "-d",
    "--wait",
  ]);
  const args = [
    "-f",
    "compose.yaml",
    "-f",
    "compose.build.yaml",
    "config",
    "--quiet",
  ];
  assert.deepEqual(run({}, args).forwarded.slice(1), ["compose", ...args]);
  assert.equal(
    run({ TEST_CONFIG_IP: "10.20.30.40", TEST_NO_ROUTE: "1" }).forwarded[0],
    "10.20.30.40",
  );
  assert.equal(
    run({
      TEST_CONFIG_IP: "172.16.1.20",
      TEST_ENDPOINT: "ssh://example.invalid",
    }).status,
    0,
  );
  assert.equal(run({ TEST_EXIT: "17" }).status, 17);
  assert.equal(
    run({
      DOCKER_CONTEXT: "local-fixture",
      DOCKER_HOST: "tcp://example.invalid:2376",
    }).status,
    0,
  );
  const configError = run({ TEST_CONFIG_ERROR: "7" });
  assert.equal(configError.status, 7);
  assert.equal(configError.forwarded, null);
  assert.equal(run({}, ["--env-file", "other.env", "up"]).forwarded, null);

  for (const env of [
    { TEST_NO_ROUTE: "1" },
    { TEST_LINK: "2: tun0: <POINTOPOINT,NOARP,UP> mtu 1500" },
    { TEST_ADDRESS: "2: eth0 inet 192.168.50.99/24 scope global eth0" },
    {
      TEST_ROUTE: "192.0.2.1 dev eth0 src 8.8.8.8",
      TEST_ADDRESS: "2: eth0 inet 8.8.8.8/24 scope global eth0",
    },
    { TEST_ENDPOINT: "ssh://example.invalid" },
    { DOCKER_HOST: "tcp://example.invalid:2376" },
    {
      DOCKER_CONTEXT: "remote-fixture",
      DOCKER_HOST: "unix:///var/run/docker.sock",
      TEST_ENDPOINT: "ssh://example.invalid",
    },
    ...[
      "127.0.0.1",
      "0.0.0.0",
      "8.8.8.8",
      "169.254.1.1",
      "fd12::1",
      "192.168.50.999",
      "192.168.050.20",
      "$(touch sentinel)",
    ].map((TEST_CONFIG_IP) => ({ TEST_CONFIG_IP })),
  ]) {
    const result = run(env);
    assert.notEqual(result.status, 0, JSON.stringify(env));
    assert.equal(
      result.forwarded,
      null,
      "invalid address must not reach Compose services",
    );
    assert.match(result.stderr, /SERVER_IP/);
  }
  assert(
    !fs.existsSync(path.join(fixture, "sentinel")),
    "address must never be evaluated as shell code",
  );
  console.log(`Server IP: ${checks} shell checks passed`);
} finally {
  assert.equal(path.dirname(fixture), path.resolve(os.tmpdir()));
  fs.rmSync(fixture, { recursive: true, force: true });
}
