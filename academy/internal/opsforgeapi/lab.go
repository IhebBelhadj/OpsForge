package opsforgeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/IhebBelhadj/opsforge/academy/contract"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// State is the lab now (`GET /v1/state`).
func (c *Client) State(ctx context.Context) (lab.State, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var s lab.State
	return s, c.call(ctx, http.MethodGet, "/v1/state", nil, "", &s, http.StatusOK)
}

// CheckSteps asks why sets of scenario steps would not run, without running them (`POST /v1/steps/validate`): the
// course compiler sends every lab's and challenge's steps.
func (c *Client) CheckSteps(ctx context.Context, sets []lab.StepSet) ([]lab.StepError, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var res struct {
		Errors []lab.StepError `json:"errors"`
	}
	in := struct {
		Contract int           `json:"contract"`
		Sets     []lab.StepSet `json:"sets"`
	}{Contract, sets}
	if in.Sets == nil {
		in.Sets = []lab.StepSet{}
	}
	return res.Errors, c.call(ctx, http.MethodPost, "/v1/steps/validate", in, "stepsOutput", &res, http.StatusOK)
}

// Run runs a lab's setup or teardown steps (a job: `POST /v1/runs`).
func (c *Client) Run(ctx context.Context, req lab.RunRequest) (lab.RunResult, error) {
	var res lab.RunResult
	return res, c.job(ctx, "/v1/runs", struct {
		Contract int `json:"contract"`
		lab.RunRequest
	}{Contract, req}, "runOutput", &res)
}

// Probe runs a probe check (a job: `POST /v1/probes`).
func (c *Client) Probe(ctx context.Context, req lab.ProbeRequest) (lab.ProbeResult, error) {
	var res lab.ProbeResult
	return res, c.job(ctx, "/v1/probes", struct {
		Contract int `json:"contract"`
		lab.ProbeRequest
	}{Contract, req}, "probeOutput", &res)
}

// Heal puts every link back to its state from before the first fault (a job: `POST /v1/heal`); false: there was
// none.
func (c *Client) Heal(ctx context.Context) (bool, error) {
	var res struct {
		Healed bool `json:"healed"`
	}
	return res.Healed, c.job(ctx, "/v1/heal", nil, "healOutput", &res)
}

// job is one lab job: started, then polled (each poll waits on the server) until done, within LabTimeout. A
// failed job is its *Problem; its output is checked against $defs/<def> and decoded into out.
func (c *Client) job(ctx context.Context, path string, in any, def string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.LabTimeout)
	defer cancel()
	var j jobDoc
	if err := c.call(ctx, http.MethodPost, path, in, "jobOutput", &j, http.StatusAccepted); err != nil {
		return err
	}
	wait := int(c.Wait / time.Second)
	for j.Status == "running" {
		err := c.call(ctx, http.MethodGet, fmt.Sprintf("/v1/jobs/%s?wait=%d", j.ID, wait), nil, "jobOutput", &j,
			http.StatusOK)
		var p *Problem
		if errors.As(err, &p) && p.Status == http.StatusNotFound {
			return fmt.Errorf("opsforge %s job %s: %w", j.Kind, j.ID, ErrJobLost)
		}
		if err != nil {
			return err
		}
	}
	if j.Status == "failed" {
		if j.Problem == nil {
			return fmt.Errorf("opsforge %s job %s failed", j.Kind, j.ID)
		}
		return j.Problem
	}
	return decode(path, j.Output, def, out)
}

type jobDoc struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Status  string          `json:"status"`
	Output  json.RawMessage `json:"output"`
	Problem *Problem        `json:"problem"`
}

func validate(raw []byte, ref string) error {
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

// schemas holds the pinned contract schema (compiled schemas are cached by the compiler).
var schemas = sync.OnceValue(func() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(contract.ToolchainSchema))
	if err != nil {
		panic(err) // embedded at build time; the document tests cover it
	}
	if err := c.AddResource("toolchain.schema.json", doc); err != nil {
		panic(err)
	}
	return c
})
