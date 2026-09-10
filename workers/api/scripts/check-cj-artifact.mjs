import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
export function checkCJArtifact(repo = fileURLToPath(new URL("../../../", import.meta.url))) {
  const source = readFileSync(resolve(repo, "internal/embedded/data/cjlogprobs.gz"));
  const packed = readFileSync(resolve(repo, "internal/embedded/data/cjmodel-v1.bin"));
  const m = JSON.parse(readFileSync(resolve(repo, "internal/embedded/data/cjmodel-v1.manifest.json"), "utf8"));
  assert.ok(packed.length >= 136 && packed.length <= 32 * 1024 * 1024, "CJ artifact length/budget");
  assert.equal(packed.subarray(0, 8).toString(), "JPQGCJ01");
  for (const [offset, value] of [[8, 1], [12, 0], [16, 0x3400], [20, 0x9fff], [24, 3], [28, 1], [48, 82944], [68, 1]]) assert.equal(packed.readUInt32LE(offset), value);
  const keys = packed.readUInt32LE(56), offsets = packed.readUInt32LE(60), probs = packed.readUInt32LE(64);
  assert.ok(keys >= 16 && (keys & (keys - 1)) === 0 && keys === offsets && probs >= 1);
  assert.equal(packed.readUInt32LE(52), keys - 1);
  assert.equal(packed.length, 136 + 82944 * 8 + keys * 4 + offsets * 4 + probs * 4);
  const content = createHash("sha256").update(packed.subarray(0, 104)).update(Buffer.alloc(32)).update(packed.subarray(136)).digest("hex");
  assert.equal(content, packed.subarray(104, 136).toString("hex"), "CJ content checksum");
  assert.equal(sha(source), packed.subarray(72, 104).toString("hex"), "CJ source changed; regenerate artifact");
  let occupied = 0;
  for (let i = 136 + 82944 * 8; i < 136 + 82944 * 8 + keys * 4; i += 4) if (packed.readUInt32LE(i)) occupied++;
  const expected = { schema_version: 1, format_version: 1, language_order_id: 1, table_layout_id: 1,
    unigram_count: 82944, keys_count: keys, offsets_count: offsets, probs_count: probs, mask: keys - 1,
    occupied_bigram_entries: occupied, payload_bytes: packed.length - 136, packed_bytes: packed.length,
    upstream_version: "1.0.5", source_file: "internal/embedded/data/cjlogprobs.gz", source_sha256: sha(source), source_bytes: source.length,
    packed_file: "internal/embedded/data/cjmodel-v1.bin", packed_file_sha256: sha(packed), content_sha256: content,
    decoded_array_bytes: packed.length - 136, embedded_plus_decoded_array_bytes: packed.length * 2 - 136,
    parser_format_revision: 1, go_version: m.go_version };
  assert.match(m.go_version, /^go\d+\.\d+(?:\.\d+)?$/);
  assert.deepEqual(m, expected, "CJ manifest mismatch");
  return m;
}
if (process.argv[1] === fileURLToPath(import.meta.url)) checkCJArtifact();
