// Shared subprocess fixture: real Go CLI, deterministic external lint tools.
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

export function proseFixture() {
  const dir = mkdtempSync(join(tmpdir(), "jpqg-harness-"));
  const names = ["JPQG_BIN", "JPQG_TEXTLINT", "JPQG_TEXTLINT_BIN", "JPQG_TEXTLINT_CONFIG", "JPQG_NATURAL_JAPANESE", "JPQG_NATURAL_JAPANESE_SCRIPT", "JPQG_NATURAL_JAPANESE_GENRE", "JPQG_UV_BIN", "JPQG_WARNINGS_AS_ERRORS"];
  const saved = Object.fromEntries(names.map((name) => [name, process.env[name]]));
  const cleanup = () => {
    for (const [name, value] of Object.entries(saved)) {
      if (value === undefined) delete process.env[name];
      else process.env[name] = value;
    }
    rmSync(dir, { recursive: true, force: true });
  };
  try {
    const bin = join(dir, "jp-quality-gate");
    execFileSync("go", ["build", "-o", bin, "./cmd/jp-quality-gate"], {
      cwd: fileURLToPath(new URL("../", import.meta.url)),
      env: process.env,
    });
    const config = join(dir, ".textlintrc.json");
    writeFileSync(config, JSON.stringify({ rules: {} }));
    const textlint = join(dir, "textlint");
    const uv = join(dir, "uv");
    writeFileSync(textlint, `#!${process.execPath}
const fs = require('node:fs');
const text = fs.readFileSync(0, 'utf8');
const index = text.indexOf('包括的');
console.log(JSON.stringify([{messages: index < 0 ? [] : [{ruleId: 'style', message: '表現を簡潔にしてください', range: [index,index+3]}]}]));
process.exit(index < 0 ? 0 : 1);
`, { mode: 0o700 });
    writeFileSync(uv, `#!${process.execPath}
const fs = require('node:fs');
const args = process.argv.slice(2);
const text = fs.readFileSync(args[args.indexOf('--script')+2], 'utf8');
console.log(JSON.stringify({findings: text.includes('包括的') ? [{line:1, category:'translationese', excerpt:'包括的', detail:'文章表現を修正してください'}] : []}));
`, { mode: 0o700 });
    for (const name of names) delete process.env[name];
    Object.assign(process.env, {
      JPQG_BIN: bin, JPQG_TEXTLINT: "1", JPQG_TEXTLINT_BIN: textlint, JPQG_TEXTLINT_CONFIG: config,
      JPQG_NATURAL_JAPANESE: "1", JPQG_NATURAL_JAPANESE_SCRIPT: uv,
      JPQG_UV_BIN: uv, JPQG_WARNINGS_AS_ERRORS: "1",
    });
    return { cleanup };
  } catch (error) {
    cleanup();
    throw error;
  }
}
