const assert = require("node:assert/strict");
const {
  readFileSync,
  writeFileSync,
  appendFileSync,
  existsSync,
} = require("node:fs");
const path = require("node:path");

process.chdir(path.resolve(__dirname, ".."));
assert(
  process.argv.length === 2 ||
    (process.argv.length === 3 && process.argv[2] === "--write"),
  "Usage: node scripts/version.cjs [--write]",
);
const write = process.argv[2] === "--write";
const version = readFileSync("internal/app/VERSION", "utf8").trim();
assert.match(
  version,
  /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/,
  "Invalid application version",
);

for (const file of [
  "package.json",
  "web/package.json",
  "web/package-lock.json",
]) {
  const source = readFileSync(file, "utf8");
  const data = JSON.parse(source);
  data.version = version;
  if (data.packages) data.packages[""].version = version;
  const expected = JSON.stringify(data, null, 2) + "\n";
  if (write) writeFileSync(file, expected);
  else
    assert.equal(
      source,
      expected,
      `${file} version differs; run npm run version:sync`,
    );
}

// shortcut: the website shows only the current release; narrow its patterns if historical versions are added.
for (const [file, pattern, replacement] of [
  [
    "README.md",
    /当前源码版本：\*\*v\d+\.\d+\.\d+\*\*/g,
    `当前源码版本：**v${version}**`,
  ],
  [
    "README.md",
    /\/releases\/tag\/v\d+\.\d+\.\d+/g,
    `/releases/tag/v${version}`,
  ],
  [
    ".env.example",
    /((?:ghcr\.io|ghcr\.nju\.edu\.cn)\/leoskey\/dns-assistant:)[\w.-]+/g,
    `$1${version}`,
  ],
  ["website/index.html", /\bv\d+\.\d+\.\d+\b/g, `v${version}`],
  [
    "website/index.html",
    /dns-assistant-\d+\.\d+\.\d+-windows/g,
    `dns-assistant-${version}-windows`,
  ],
]) {
  const source = readFileSync(file, "utf8");
  assert.match(source, pattern, `${file} version field missing`);
  const expected = source.replace(pattern, replacement);
  if (write) writeFileSync(file, expected);
  else
    assert.equal(
      source,
      expected,
      `${file} version differs; run npm run version:sync`,
    );
}

if (!write) {
  const website = readFileSync("website/index.html", "utf8");
  for (const suffix of ["-setup.exe", ".zip"]) {
    assert(
      website.includes(
        `/releases/download/v${version}/dns-assistant-${version}-windows-amd64${suffix}`,
      ),
      "Website download version missing or inconsistent",
    );
  }
  assert.match(
    website,
    new RegExp(
      `class="version"\\s*>v${version.replaceAll(".", "\\.")}<\\/span`,
    ),
    "Website footer version missing or inconsistent",
  );
  assert(
    existsSync(`docs/releases/v${version}.md`),
    "Current release notes missing",
  );
  if (process.env.RELEASE_REF?.startsWith("refs/tags/")) {
    assert.equal(
      process.env.RELEASE_REF,
      `refs/tags/v${version}`,
      "Git tag differs from application version",
    );
  }
  if (process.env.GITHUB_OUTPUT)
    appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\n`);
}
console.log(
  `${write ? "Synchronized" : "Checked"} application version ${version}`,
);
