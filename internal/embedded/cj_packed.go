//go:build jpqg_packed_cjmodel

package embedded

import _ "embed"

// CJModelPacked is the generated v1 CJ model; canonical gzip remains in source control.
//
//go:embed data/cjmodel-v1.bin
var CJModelPacked []byte
