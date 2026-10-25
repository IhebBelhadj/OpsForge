// Package lab is the lab as the platform sees it: the documents of the toolchain contract (docs/courses/
// architecture.md, "The lab toolchain contract"; academy/api/toolchain.schema.json). Modules declare the ports they
// need over these types (labsession, labstate, challenge); internal/opsforgecli implements them. Pure data.
package lab

import "encoding/json"

// State is the lab now, as a mimic panel shows it (`academy state`).
type State struct {
	Up       bool            `json:"up"`
	Error    *string         `json:"error"`
	Topology string          `json:"topology"`
	Layout   *string         `json:"layout"`
	Sites    map[string]Site `json:"sites"`
	Links    map[string]Link `json:"links"`
	Nodes    []Node          `json:"nodes"`
	Fault    bool            `json:"fault"`
	// Placeholders are the values of the lab-text placeholders in the running topology (scenario.placeholders):
	// "${node:a}" → "a-cp1", "${ip:a-cp1}" → "10.10.0.11", ...
	Placeholders map[string]string `json:"placeholders"`
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

// Phase is when a lab's steps run: setup before the learner starts, teardown after a finish or an abandon.
type Phase string

// The phases of `academy run`.
const (
	Setup    Phase = "setup"
	Teardown Phase = "teardown"
)

// RunRequest runs a lab's setup or teardown steps. Steps are scenario steps as the bundle holds them (validated when
// the pack was compiled; the toolchain resolves their placeholders). A Namespace (lab-<attempt>, ch-<attempt>) is
// created before setup and deleted after teardown, and fills `${ns}`.
type RunRequest struct {
	Phase     Phase             `json:"phase"`
	Steps     []json.RawMessage `json:"steps"`
	Namespace string            `json:"namespace,omitempty"`
}

// RunResult is what the steps measured. Error says why the run stopped early ("" when every step ran).
type RunResult struct {
	Metrics map[string]float64 `json:"metrics"`
	Log     []string           `json:"log"`
	Error   string             `json:"error"`
}

// ProbeRequest is a probe check: steps, then expectations on their metrics. Expect stays raw so its order (the
// order of the results) is the author's.
type ProbeRequest struct {
	Steps     []json.RawMessage `json:"steps"`
	Expect    json.RawMessage   `json:"expect"`
	Namespace string            `json:"namespace,omitempty"`
}

// ProbeResult is a check's verdict, one Expectation per condition. The probe stops at the first failing named step,
// so expectations on later steps are not Checked.
type ProbeResult struct {
	Passed  bool          `json:"passed"`
	Results []Expectation `json:"results"`
	Log     []string      `json:"log"`
	Error   string        `json:"error"`
}

// Expectation is one condition on one metric (Want: "<op> <value>"); Got is nil when nothing was measured.
type Expectation struct {
	Metric  string   `json:"metric"`
	Want    string   `json:"want"`
	Got     *float64 `json:"got"`
	Passed  bool     `json:"passed"`
	Checked bool     `json:"checked"`
}

// StepSet is scenario steps to check without running them (`api validate-steps`): a lab's setup or teardown, a
// check's probe with its Expect. NoFaults refuses fault steps (partition, degrade, ...): a check only observes the
// lab.
type StepSet struct {
	Steps    []any
	Expect   map[string]any
	NoFaults bool
}

// MarshalJSON writes the contract's stepsInput set: `faults: false` only when refused, `expect` only when given.
func (s StepSet) MarshalJSON() ([]byte, error) {
	doc := map[string]any{"steps": s.Steps}
	if s.Steps == nil {
		doc["steps"] = []any{}
	}
	if s.Expect != nil {
		doc["expect"] = s.Expect
	}
	if s.NoFaults {
		doc["faults"] = false
	}
	return json.Marshal(doc)
}

// StepError is why the steps of the Set-th StepSet would not run ("steps[2]: ..." or "expect: <metric>: ...").
type StepError struct {
	Set     int    `json:"set"`
	Message string `json:"message"`
}
