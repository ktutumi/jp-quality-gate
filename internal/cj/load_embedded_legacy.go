//go:build !jpqg_packed_cjmodel

package cj

import "github.com/ktutumi/jp-quality-gate/internal/embedded"

func loadBundledModel() (*Classifier, error) {
	return loadGZIPModel(embedded.CJModelGZIP, "bundled:cjlogprobs.gz", 0)
}
