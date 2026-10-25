package opsforgecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/IhebBelhadj/opsforge/academy/contract"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// State runs `api state`.
func (r *Runner) State(ctx context.Context) (lab.State, error) {
	var s lab.State
	return s, r.Call(ctx, "state", nil, &s)
}

// CheckSteps asks why sets of scenario steps would not run, without running them (`api validate-steps`): the course
// compiler sends every lab's and challenge's steps.
func (r *Runner) CheckSteps(ctx context.Context, sets []lab.StepSet) ([]lab.StepError, error) {
	var res struct {
		Errors []lab.StepError `json:"errors"`
	}
	return res.Errors, r.checked(ctx, r.Timeout, "validate-steps", struct {
		Contract int           `json:"contract"`
		Sets     []lab.StepSet `json:"sets"`
	}{Contract, sets}, "stepsOutput", &res)
}

// Run runs a lab's setup or teardown steps (`api run`).
func (r *Runner) Run(ctx context.Context, req lab.RunRequest) (lab.RunResult, error) {
	var res lab.RunResult
	return res, r.lab(ctx, "run", struct {
		Contract int `json:"contract"`
		lab.RunRequest
	}{Contract, req}, "runOutput", &res)
}

// Probe runs a probe check (`api probe`).
func (r *Runner) Probe(ctx context.Context, req lab.ProbeRequest) (lab.ProbeResult, error) {
	var res lab.ProbeResult
	return res, r.lab(ctx, "probe", struct {
		Contract int `json:"contract"`
		lab.ProbeRequest
	}{Contract, req}, "probeOutput", &res)
}

// Heal puts every link back to its state from before the first fault (`api heal`); false: there was none.
func (r *Runner) Heal(ctx context.Context) (bool, error) {
	var res struct {
		Healed bool `json:"healed"`
	}
	return res.Healed, r.lab(ctx, "heal", nil, "healOutput", &res)
}

// lab calls a command that acts on the lab, with the lab timeout.
func (r *Runner) lab(ctx context.Context, command string, in any, def string, out any) error {
	return r.checked(ctx, r.LabTimeout, command, in, def, out)
}

// checked calls a command and checks its output against toolchain.schema.json's $defs/<def> before decoding it.
func (r *Runner) checked(ctx context.Context, timeout time.Duration, command string, in any, def string, out any) error {
	var raw json.RawMessage
	if err := r.call(ctx, timeout, command, in, &raw); err != nil {
		return err
	}
	if err := validate(raw, "toolchain.schema.json#/$defs/"+def); err != nil {
		return fmt.Errorf("toolchain %s: %w: %w", command, ErrInvalidDocument, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("toolchain %s: %w", command, err)
	}
	return nil
}

func validate(raw json.RawMessage, ref string) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	s, err := schemas().Compile(ref)
	if err != nil {
		return err
	}
	return s.Validate(doc)
}

// schemas holds the embedded contract schemas (compiled schemas are cached by the compiler).
var schemas = sync.OnceValue(func() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	for name, data := range map[string][]byte{
		"toolchain.schema.json": contract.ToolchainSchema,
	} {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			panic(err) // embedded at build time; the lab document tests cover it
		}
		if err := c.AddResource(name, doc); err != nil {
			panic(err)
		}
	}
	return c
})
