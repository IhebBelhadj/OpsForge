// Package contract is Academy's pinned copy of OpsForge's toolchain contract (README.md): the schema and the golden
// documents of the OpsForge release in PINNED, copied by `make contract-update`. Never edit the copy: change OpsForge,
// then update it. The adapter (internal/opsforgeapi) checks outputs against the schema and its tests decode the goldens.
package contract

import _ "embed"

// ToolchainSchema is the JSON Schema of the documents, one $defs entry each (outputs are open: unknown fields are
// ignored).
//
//go:embed toolchain.schema.json
var ToolchainSchema []byte
