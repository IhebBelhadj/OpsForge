// Package api holds the platform's contracts: openapi.yaml (HTTP, generates internal/httpapi and the web client)
// and bundle.schema.json (what `opsforge academy compile` prints).
package api

import _ "embed"

// BundleSchema is the JSON Schema of a compiled content bundle (contract 1).
//
//go:embed bundle.schema.json
var BundleSchema []byte
