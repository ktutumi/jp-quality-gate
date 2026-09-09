import { copyFileSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const workerDir = resolve(scriptDir, "..");
const repositoryDir = resolve(workerDir, "../..");
const generatedDir = join(workerDir, ".generated");
const distDir = join(workerDir, "dist");
const wasmPath = join(generatedDir, "core.wasm");

function run(command, args, cwd, env = process.env) {
  execFileSync(command, args, { cwd, env, stdio: "inherit" });
}

mkdirSync(generatedDir, { recursive: true });
run(
  "go",
  ["build", "-o", wasmPath, "./cmd/jp-quality-gate-wasm"],
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
rmSync(distDir, { recursive: true, force: true });

run(
  "wrangler",
  ["deploy", "--dry-run", "--config", "wrangler.validation.jsonc", "--outdir", "dist"],
  workerDir,
);
