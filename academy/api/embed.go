// Package api holds the platform's contracts: openapi.yaml (HTTP, generates internal/httpapi and the web client),
// bundle.schema.json (what `opsforge academy compile` prints) and toolchain.schema.json (run, probe, heal).
package api

import _ "embed"

// BundleSchema is the JSON Schema of a compiled content bundle (contract 1).
//
//go:embed bundle.schema.json
var BundleSchema []byte

// ToolchainSchema is the JSON Schema of the run, probe and heal documents (contract 1), one $defs entry each.
//
//go:embed toolchain.schema.json
var ToolchainSchema []byte
