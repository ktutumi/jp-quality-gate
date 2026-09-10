//go:build jpqg_packed_cjmodel

package cj

import "github.com/ktutumi/jp-quality-gate/internal/embedded"

func loadBundledModel() (*Classifier, error) { return LoadPacked(embedded.CJModelPacked) }
