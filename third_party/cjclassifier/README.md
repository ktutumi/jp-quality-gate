# CJClassifier attribution

`jp-quality-gate` includes a pure Go port of the CJClassifier model and classifier behavior. This directory records the third-party license and provenance for that port and its bundled model.

## Provenance

- Upstream project: [jlpka/cjclassifier](https://github.com/jlpka/cjclassifier)
- Pinned upstream commit: [`859c2bdca31c83b30a9d626fd04e2a79f081e61a`](https://github.com/jlpka/cjclassifier/commit/859c2bdca31c83b30a9d626fd04e2a79f081e61a)
- Python package version: `cjclassifier==1.0.5`
- Model source: `cjclassifier/cjlogprobs.gz` from the `cjclassifier==1.0.5` distribution
- Upstream model path: `core/src/main/resources/com/jlpka/cjclassifier/cjlogprobs.gz`
- Model SHA-256: `b0fcb1e82dac11d2e11710012b563f7b19ee3e92ce6a01e7de806bcaadfc012f`
- Upstream source copyright: Copyright 2026 Jeremy Lilley (`jeremy@jlilley.net`)
- License: Apache License 2.0; see [LICENSE](LICENSE)

The Go implementation in `jp-quality-gate` is unofficial and is not an official Go implementation maintained by the upstream project.

The model contains statistical parameters derived from Chinese and Japanese Wikipedia corpora. It does not contain or reproduce Wikipedia text. See [NOTICE](NOTICE) for the related attribution.

## Upstream reference

See the [pinned upstream commit](https://github.com/jlpka/cjclassifier/commit/859c2bdca31c83b30a9d626fd04e2a79f081e61a) for the source project and its Apache-2.0 licensing terms.

## Worker packed derivative

`internal/embedded/data/cjmodel-v1.bin` is a deterministic, uncompressed derivative
of the canonical CJClassifier 1.0.5 gzip listed above. The existing Go `ParseModel`
parser supplies the final arrays; float64 unigrams/scalars, float32 bigrams, hash
slots and offsets are preserved bit-for-bit. No model retraining, quantization,
probability change, or language reordering is applied.

Run `make pack-cj` to generate and `make check-cj-packed` to verify complete
regeneration. The adjacent JSON manifest records source/file/content hashes,
counts, format revision, and Go version. See [the v1 format](../../internal/cj/PACKED.md).
The existing Apache-2.0 [LICENSE](LICENSE) and [NOTICE](NOTICE) apply to this derivative.
Normal native CLI builds retain the canonical gzip; only the explicit
`jpqg_packed_cjmodel` build tag selects the generated model.
