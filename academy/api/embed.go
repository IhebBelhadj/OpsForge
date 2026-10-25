// Package api holds the platform's own contracts: openapi.yaml (HTTP, generates internal/httpapi and the web client)
// and bundle.schema.json (a compiled content bundle). OpsForge's toolchain contract is pinned in academy/contract.
package api

import _ "embed"

// BundleSchema is the JSON Schema of a compiled content bundle (contract 1).
//
//go:embed bundle.schema.json
var BundleSchema []byte
