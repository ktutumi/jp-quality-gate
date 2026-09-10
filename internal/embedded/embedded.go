package embedded

import _ "embed"

// UnihanTableGZIP is the Unicode 18.0.0 default quality-gate table.
//
//go:embed data/unihan-suspicious-18.0.0.json.gz
var UnihanTableGZIP []byte
