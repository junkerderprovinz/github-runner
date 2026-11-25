/**
 * Renders github-runner-2.png: the web interface with five made-up repositories,
 * served by the real service against a fake GitHub API and a fake runner.
 * Run: node .github/assets/screenshots/render-webui.mjs (needs Go, bash and a global playwright-core)
 */
import { mkdirSync, mkdtempSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { execSync, spawn } from "node:child_process";
import http from "node:http";
import net from "node:net";

const require = createRequire(import.meta.url);
const { chromium } = require(`${execSync("npm root -g").toString().trim()}/playwright-core`);
const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "..", "..", "..");
const TMP = mkdtempSync(join(tmpdir(), "github-runner-shot-"));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const freePort = () =>
  new Promise((resolve) => {
    const s = net.createServer().listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });

// The runner stands in for the real one: it registers, prints what the real
// one prints, and starts a job in website at once and in homelab after 29 s.
// It sleeps in one-second steps so nothing lingers once it is stopped.
const home = join(TMP, "home");
mkdirSync(home);
writeFileSync(join(home, "config.sh"), `#!/bin/bash
if [ "$1" = remove ]; then rm -f .runner; exit 0; fi
touch .runner
`);
writeFileSync(join(home, "run.sh"), `#!/bin/bash
nap() { for _ in $(seq "$1"); do sleep 1; done; }
stamp() { date -u '+%Y-%m-%d %H:%M:%SZ'; }
echo "√ Connected to GitHub"
echo "Current runner version: '2.338.2'"
echo "$(stamp): Listening for Jobs"
job() { nap "$1"; echo "$(stamp): Running job: $2"; }
case $(basename "$PWD") in
  you__website) job 1 deploy ;;
  you__homelab) job 29 backup-check ;;
esac
nap 3600
`);

const now = Date.now();
const min = 60_000;
const iso = (t) => new Date(t).toISOString();
const repos = ["you/website", "you/backend", "you/dotfiles", "you/homelab", "you/notes"];
const config = join(TMP, "config");
mkdirSync(config);
writeFileSync(join(config, "config.json"), JSON.stringify({
  token: "github_pat_11ABCDEFG0fakefakefake4f2a", owner: "you", name: "GitHub-Runner", labels: ["self-hosted", "docker"],
  noDefaultLabels: false, runAsRoot: true, deregisterOnStop: true, retrySeconds: 300,
  repos: repos.map((name, line) => ({ name, line, labels: name === "you/backend" ? ["self-hosted", "docker", "gpu"] : null, added: iso(now - 30 * 60 * min) })),
  seen: {},
}));
// [repository, minutes since it ended, seconds it took, result, workflow, job, branch]
const past = [
  ["you/website", 1500, 64, "passed", "Deploy", "deploy", "main"], ["you/website", 300, 120, "passed", "CI", "test", "main"],
  ["you/website", 190, 78, "failed", "Deploy", "deploy", "dev"], ["you/website", 130, 20, "passed", "Lint", "lint", "main"],
  ["you/website", 88, 69, "passed", "Deploy", "deploy", "main"], ["you/website", 33, 108, "passed", "CI", "test", "main"],
  ["you/backend", 400, 95, "passed", "CI", "build", "main"], ["you/backend", 106, 91, "passed", "CI", "build", "main"],
  ["you/backend", 74, 143, "failed", "CI", "test", "dev"], ["you/backend", 41, 88, "passed", "CI", "build", "main"],
  ["you/backend", 26, 140, "passed", "CI", "test", "main"], ["you/backend", 15, 24, "passed", "Lint", "lint", "main"],
  ["you/dotfiles", 113, 22, "passed", "Lint", "lint", "main"], ["you/dotfiles", 64, 30, "passed", "Lint", "lint", "main"],
  ["you/dotfiles", 50, 31, "cancelled", "Lint", "lint", "dev"], ["you/dotfiles", 46, 24, "passed", "Lint", "lint", "main"],
  ["you/homelab", 1440, 61, "passed", "Nightly", "backup-check", "main"], ["you/homelab", 104, 180, "passed", "Release", "publish", "main"],
  ["you/homelab", 82, 92, "failed", "CI", "build", "main"], ["you/homelab", 57, 70, "passed", "Deploy", "deploy", "main"],
  ["you/homelab", 23, 140, "passed", "CI", "test", "main"], ["you/homelab", 14, 58, "passed", "Nightly", "backup-check", "main"],
];
writeFileSync(join(config, "history.json"), JSON.stringify({
  jobs: past.map(([repo, ago, took, result, workflow, name, branch], i) => {
    const ended = now - ago * min;
    return { id: i + 1, repo, name, workflow, branch, started: iso(ended - took * 1000), ended: iso(ended), result };
  }),
}));

// you/notes gets the refusal a token without administration rights gets.
const flows = { "you/website": ["Deploy", "main", "deploy"], "you/homelab": ["Nightly", "main", "backup-check"] };
const reply = (res, code, body) => res.writeHead(code, { "content-type": "application/json" }).end(JSON.stringify(body));
const github = http.createServer((req, res) => {
  const path = new URL(req.url, "http://x").pathname;
  let m;
  if ((m = path.match(/^\/repos\/([^/]+\/[^/]+)\/actions\/runners\/(registration|remove)-token$/))) {
    return m[1] === "you/notes" ? reply(res, 403, {}) : reply(res, 201, { token: "T" });
  }
  if ((m = path.match(/^\/repos\/([^/]+\/[^/]+)\/actions\/runs$/))) {
    const [name, branch] = flows[m[1]] ?? ["CI", "main"];
    return reply(res, 200, { workflow_runs: [{ id: 1, name, head_branch: branch }] });
  }
  if ((m = path.match(/^\/repos\/([^/]+\/[^/]+)\/actions\/runs\/1\/jobs$/))) {
    const [, , job] = flows[m[1]] ?? [];
    return reply(res, 200, { jobs: [{ name: job, runner_name: "GitHub-Runner", status: "in_progress", started_at: new Date().toISOString() }] });
  }
  reply(res, 404, {});
});
const apiPort = await freePort();
await new Promise((r) => github.listen(apiPort, "127.0.0.1", r));

if (!existsSync(join(ROOT, "web", "node_modules"))) execSync("npm ci --no-audit --no-fund", { cwd: join(ROOT, "web"), stdio: "inherit" });
execSync("npm run build", { cwd: join(ROOT, "web"), stdio: "inherit" });
const bin = join(TMP, process.platform === "win32" ? "github-runner.exe" : "github-runner");
execSync(`go build -o "${bin}" ./cmd/github-runner`, { cwd: ROOT, stdio: "inherit" });

const port = await freePort();
const service = spawn(bin, [
  "-config", config, "-runner-home", home, "-runners", join(TMP, "runners"), "-work", join(TMP, "work"),
  "-api", `http://127.0.0.1:${apiPort}`, "-banner", join(TMP, "none"),
], { env: { ...process.env, WEBUI_PORT: String(port), ACCESS_TOKEN: "" }, stdio: ["ignore", "inherit", "inherit"] });
const base = `http://127.0.0.1:${port}/`;

try {
  // The picture shows homelab's job 14 s in, website's about half a minute later.
  for (;;) {
    const state = await fetch(base + "api/state").then((r) => r.json()).catch(() => null);
    const job = state?.repos.find((r) => r.repo === "you/homelab")?.job;
    if (job && Date.now() - Date.parse(job.started) >= 14_000) break;
    await sleep(500);
  }
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: "light", deviceScaleFactor: 2 });
  await page.goto(base + "#/");
  await page.waitForSelector(".a-timeline");
  await sleep(1200);
  const box = await page.locator(".a-timeline").boundingBox();
  await page.screenshot({ path: join(HERE, "github-runner-2.png"), clip: { x: 0, y: 0, width: 1440, height: Math.ceil(box.y + box.height + 24) } });
  await browser.close();
  console.log("github-runner-2.png 1440 wide at 2x");
} finally {
  if (process.platform === "win32") {
    execSync(`taskkill /T /F /PID ${service.pid}`, { stdio: "ignore" });
  } else {
    service.kill("SIGTERM");
    await new Promise((r) => service.once("exit", r));
  }
  github.close();
  await sleep(1500);
  rmSync(TMP, { recursive: true, force: true, maxRetries: 10, retryDelay: 500 });
}
