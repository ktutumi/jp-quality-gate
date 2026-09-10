// Exercise missing/stale artifacts in a disposable copy, never the working tree.
import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
const root = fileURLToPath(new URL("../../../", import.meta.url));
const temp = mkdtempSync(join(tmpdir(), "jpqg-bootstrap-"));
const env = { ...process.env };
for (const key of ["GOFLAGS", "GOOS", "GOARCH"]) delete env[key];
function go(args, success = true) {
  const p = spawnSync("go", args, { cwd: temp, env, encoding: "utf8", timeout: 120_000 });
  if (p.error) throw p.error;
  assert.equal(p.status === 0, success, `go ${args.join(" ")}: ${p.stderr}`);
  return p.stdout;
}
try {
  cpSync(join(root, "go.mod"), join(temp, "go.mod"));
  for (const dir of ["internal", "cmd"]) cpSync(join(root, dir), join(temp, dir), { recursive: true,
    filter: (path) => !path.endsWith("cjmodel-v1.bin") && !path.endsWith("cjmodel-v1.manifest.json") });
  go(["list", "-tags=jpqg_packed_cjmodel", "./internal/embedded"], false);
  go(["run", "./cmd/jpqg-pack-cjmodel"]);
  go(["run", "./cmd/jpqg-pack-cjmodel", "--check"]);
  for (const [tag, expected, excluded] of [[null, "data/cjlogprobs.gz", "data/cjmodel-v1.bin"],
    ["jpqg_packed_cjmodel", "data/cjmodel-v1.bin", "data/cjlogprobs.gz"]]) {
    const info = JSON.parse(go(["list", "-json", ...(tag ? [`-tags=${tag}`] : []), "./internal/embedded"]));
    assert.ok(info.EmbedFiles.includes(expected) && !info.EmbedFiles.includes(excluded));
  }
  const parser = join(temp, "internal/cj/classifier.go");
  const source = readFileSync(parser, "utf8");
  const needle = "value, parseErr := strconv.ParseFloat(parts[column+1], 64)";
  assert.ok(source.includes(needle));
  writeFileSync(parser, source.replace(needle, `${needle}\n value -= 0.01`));
  go(["run", "./cmd/jpqg-pack-cjmodel", "--check"], false);
  console.log("CJ bootstrap, exclusive embed selection, and stale parser checks passed");
} finally { rmSync(temp, { recursive: true, force: true }); }
