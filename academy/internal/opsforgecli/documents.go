package opsforgecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/IhebBelhadj/opsforge/academy/api"
)

// LabState is `academy state`: the lab as a mimic panel shows it.
type LabState struct {
	Up       bool            `json:"up"`
	Error    *string         `json:"error"`
	Topology string          `json:"topology"`
	Layout   *string         `json:"layout"`
	Sites    map[string]Site `json:"sites"`
	Links    map[string]Link `json:"links"`
	Nodes    []Node          `json:"nodes"`
	Fault    bool            `json:"fault"`
}

// Site is one region: its public address and VPC.
type Site struct {
	PublicIP string `json:"public_ip"`
	VPC      string `json:"vpc"`
}

// Link is a WAN link now (Live) and as topology.yml has it (Desired); Condition is ok, changed or cut.
type Link struct {
	Live      LinkConditions `json:"live"`
	Desired   LinkConditions `json:"desired"`
	Condition string         `json:"condition"`
}

// LinkConditions are a link's netem settings.
type LinkConditions struct {
	RTT    string `json:"rtt"`
	Jitter string `json:"jitter,omitempty"`
	Loss   string `json:"loss,omitempty"`
	Rate   string `json:"rate,omitempty"`
	MTU    int    `json:"mtu,omitempty"`
}

// Node is one lab instance.
type Node struct {
	Name   string `json:"name"`
	Site   string `json:"site"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// State runs `academy state`.
func (r *Runner) State(ctx context.Context) (LabState, error) {
	var s LabState
	return s, r.Run(ctx, "state", nil, &s)
}

// Compile runs `academy compile` over the repository's packs and returns the bundle, checked against
// bundle.schema.json (the importer decodes it).
func (r *Runner) Compile(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := r.Run(ctx, "compile", nil, &raw); err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("toolchain compile: %w", err)
	}
	if err := bundleSchema().Validate(doc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidBundle, err)
	}
	return raw, nil
}

var bundleSchema = sync.OnceValue(func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(api.BundleSchema))
	if err != nil {
		panic(err) // embedded at build time; TestCompileValidatesTheBundle covers it
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("bundle.schema.json", doc); err != nil {
		panic(err)
	}
	return c.MustCompile("bundle.schema.json")
})
