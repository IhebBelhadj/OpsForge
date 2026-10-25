// Package api holds the platform's own contracts: openapi.yaml (HTTP, generates internal/httpapi and the web client)
// bundle.schema.json (a compiled content bundle) and the pack formats authors write, course.schema.json and
// labpack.schema.json (internal/compiler). OpsForge's toolchain contract is pinned in academy/contract.
package api

import _ "embed"

// BundleSchema is the JSON Schema of a compiled content bundle (contract 1).
//
//go:embed bundle.schema.json
var BundleSchema []byte

// CourseSchema is the JSON Schema of one loaded course folder of a pack.
//
//go:embed course.schema.json
var CourseSchema []byte

// LabpackSchema is the JSON Schema of a loaded pack: pack.yml, curricula, labs and badges.
//
//go:embed labpack.schema.json
var LabpackSchema []byte
