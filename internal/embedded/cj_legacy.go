//go:build !jpqg_packed_cjmodel

package embedded

import _ "embed"

// CJModelGZIP is the cjclassifier 1.0.5 text model.
//
//go:embed data/cjlogprobs.gz
var CJModelGZIP []byte
