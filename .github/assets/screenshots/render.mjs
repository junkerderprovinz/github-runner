/**
 * Renders github-runner-1.png: the end of the container log after a start, in
 * the look of Unraid's log window. The repository names are made up, the lines
 * are the ones start-runners.sh and the runner print.
 * Run: node .github/assets/screenshots/render.mjs
 */
import { readFileSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";

const require = createRequire(import.meta.url);
const { Resvg } = require(`${execSync("npm root -g").toString().trim()}/@resvg/resvg-js`);
const HERE = dirname(fileURLToPath(import.meta.url));

const banner = readFileSync(join(HERE, "..", "banner-raw.txt"), "utf8").replace(/\r/g, "").split("\n").slice(-6);
const repos = ["you/website", "you/backend", "you/dotfiles"];
const t = (s) => `2026-11-24 21:14:${String(s).padStart(2, "0")}Z`;
const lines = [
  ...banner.map((l) => ({ text: l, color: "#c9d1d9" })),
  { text: "" },
  { text: "  GitHub-Runner · one runner per repository", color: "#db61a2" },
  { text: "" },
  { text: `[github-runner] Starting 3 runner(s): ${repos.join(" ")}`, color: "#8b949e" },
  ...repos.map((r) => ({ text: `[github-runner] ${r}: registered as GitHub-Runner.`, color: "#8b949e" })),
  ...repos.map((r) => ({ text: `[${r}] √ Connected to GitHub`, color: "#3fb950" })),
  ...repos.map((r, i) => ({ text: `[${r}] ${t(7 + i)}: Listening for Jobs`, color: "#e6edf3" })),
];

const FS = 15, LH = 22, PAD = 28, CW = FS * 0.55;
const cols = Math.max(...lines.map((l) => l.text.length));
const W = Math.ceil(PAD * 2 + cols * CW), H = PAD * 2 + lines.length * LH;
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
<rect width="${W}" height="${H}" rx="10" fill="#0d1117"/>
${lines.map((l, i) => `<text x="${PAD}" y="${PAD + (i + 0.8) * LH}" font-family="Consolas, monospace" font-size="${FS}" fill="${l.color ?? "#c9d1d9"}" xml:space="preserve">${esc(l.text)}</text>`).join("\n")}
</svg>`;
writeFileSync(join(HERE, "github-runner-1.png"), new Resvg(svg, { fitTo: { mode: "width", value: 1600 }, font: { loadSystemFonts: true, defaultFontFamily: "Consolas" } }).render().asPng());
console.log(`github-runner-1.png ${W}x${H}`);
