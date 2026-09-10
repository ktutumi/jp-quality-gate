import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtempSync, mkdirSync, copyFileSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { checkCJArtifact } from "../scripts/check-cj-artifact.mjs";

test("artifact check rejects missing and independently modified source, binary, and manifest", () => {
  const root = mkdtempSync(join(tmpdir(), "jpqg-artifact-"));
  const rel = "internal/embedded/data";
  mkdirSync(join(root, rel), { recursive: true });
  try {
    assert.throws(() => checkCJArtifact(root));
    for (const name of ["cjlogprobs.gz", "cjmodel-v1.bin", "cjmodel-v1.manifest.json"]) {
      copyFileSync(fileURLToPath(new URL(`../../../${rel}/${name}`, import.meta.url)), join(root, rel, name));
    }
    checkCJArtifact(root);
    for (const name of ["cjlogprobs.gz", "cjmodel-v1.bin", "cjmodel-v1.manifest.json"]) {
      const path = join(root, rel, name), original = readFileSync(path), changed = Buffer.from(original);
      changed[Math.floor(changed.length / 2)] ^= 1;
      writeFileSync(path, changed);
      assert.throws(() => checkCJArtifact(root), name);
      assert.deepEqual(readFileSync(path), changed, "check must be read-only");
      writeFileSync(path, original);
    }
  } finally { rmSync(root, { recursive: true, force: true }); }
});
