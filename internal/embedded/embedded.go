package embedded

import _ "embed"

// CJModelGZIP is the cjclassifier 1.0.5 text model.
//
//go:embed data/cjlogprobs.gz
var CJModelGZIP []byte

// UnihanTableGZIP is the Unicode 18.0.0 default quality-gate table.
//
//go:embed data/unihan-suspicious-18.0.0.json.gz
var UnihanTableGZIP []byte
