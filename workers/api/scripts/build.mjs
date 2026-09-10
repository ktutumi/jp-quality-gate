import { createHash } from "node:crypto";
import { checkCJArtifact } from "./check-cj-artifact.mjs";
import { copyFileSync, mkdirSync, rmSync, readFileSync, writeFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const workerDir = resolve(scriptDir, "..");
const repositoryDir = resolve(workerDir, "../..");
const variant = process.env.JPQG_CJ_VARIANT ?? "packed";
if (!["legacy", "packed"].includes(variant)) throw new Error("unknown CJ variant");
if (process.env.GOFLAGS) throw new Error("unset GOFLAGS for reproducible Worker builds");
const model = variant === "packed" ? checkCJArtifact(repositoryDir) : null;
if (model && model.go_version !== execFileSync("go", ["env", "GOVERSION"], { encoding: "utf8" }).trim()) {
  throw new Error("CJ artifact toolchain differs; run make check-cj-packed and review regeneration");
}
const generatedDir = join(workerDir, ".generated", variant);
const distDir = join(workerDir, "dist", variant);
const wasmPath = join(generatedDir, "core.wasm");

function run(command, args, cwd, env = process.env) {
  execFileSync(command, args, { cwd, env, stdio: "inherit" });
}

mkdirSync(generatedDir, { recursive: true });
run(
  "go",
  ["build", ...(variant === "packed" ? ["-tags=jpqg_packed_cjmodel"] : []), "-o", wasmPath, "./cmd/jp-quality-gate-wasm"],
  repositoryDir,
  { ...process.env, CGO_ENABLED: "0", GOARCH: "wasm", GOOS: "js" },
);

const goRoot = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();
const runtimeCandidates = [
  join(goRoot, "lib", "wasm", "wasm_exec.js"),
  join(goRoot, "misc", "wasm", "wasm_exec.js"),
];
const runtimePath = runtimeCandidates.find((candidate) => {
  try {
    copyFileSync(candidate, join(generatedDir, "wasm_exec.js"));
    return true;
  } catch {
    return false;
  }
});
if (!runtimePath) {
  throw new Error("installed Go wasm_exec.js was not found");
}
copyFileSync(join(goRoot, "LICENSE"), join(generatedDir, "LICENSE"));
writeFileSync(join(generatedDir, "index.ts"), readFileSync(join(workerDir, "src/index.ts"), "utf8").replaceAll("../.generated/packed/", "./"));
rmSync(distDir, { recursive: true, force: true });

run(
  "wrangler",
  ["deploy", join(generatedDir, "index.ts"), "--dry-run", "--config", "wrangler.validation.jsonc", "--outdir", distDir],
  workerDir,
);

const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
const modules = readdirSync(distDir).filter((name) => name.endsWith(".wasm") || name === "index.js");
const files = Object.fromEntries(modules.map((name) => { const b = readFileSync(join(distDir, name)); return [name, { bytes: b.length, sha256: sha(b) }]; }));
const bytes = Object.values(files).reduce((n, file) => n + file.bytes, 0);
if (bytes >= 64 * 1024 * 1024) throw new Error("Worker bundle exceeds 64 MiB");
writeFileSync(join(distDir, "build-manifest.json"), JSON.stringify({ variant, build_tag: variant === "packed" ? "jpqg_packed_cjmodel" : null,
  commit: execFileSync("git", ["rev-parse", "HEAD"], { cwd: repositoryDir, encoding: "utf8" }).trim(),
  working_tree_dirty: execFileSync("git", ["status", "--porcelain"], { cwd: repositoryDir, encoding: "utf8" }).trim().length > 0,
  go_version: execFileSync("go", ["version"], { encoding: "utf8" }).trim(),
  gomemlimit: "48MiB", config_sha256: sha(readFileSync(join(workerDir, "wrangler.validation.jsonc"))),
  source_sha256: sha(readFileSync(join(repositoryDir, "internal/embedded/data/cjlogprobs.gz"))), model,
  glue_sha256: sha(readFileSync(join(generatedDir, "wasm_exec.js"))), files, module_bytes: bytes }, null, 2) + "\n");
