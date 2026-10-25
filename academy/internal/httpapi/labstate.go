package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/labstate"
)

// Lab is what the transport needs from the labstate module.
type Lab interface {
	State(ctx context.Context) (lab.State, error)
	Resolver(ctx context.Context, extra map[string]string) func(string) string
}

// resolver resolves lab texts for the running topology, and "${ns}" as namespace when there is one; without the
// labstate module the texts stay as written. The state is read on the first text, not before.
func (s *server) resolver(ctx context.Context, namespace string) func(string) string {
	if s.deps.Lab == nil {
		return func(t string) string { return t }
	}
	var extra map[string]string
	if namespace != "" {
		extra = map[string]string{"${ns}": namespace}
	}
	var resolve func(string) string
	return func(t string) string {
		if !strings.Contains(t, "${") {
			return t
		}
		if resolve == nil {
			resolve = s.deps.Lab.Resolver(ctx, extra)
		}
		return resolve(t)
	}
}

// GetLabState is the lab now.
func (s *server) GetLabState(ctx context.Context, _ GetLabStateRequestObject) (GetLabStateResponseObject, error) {
	st, err := s.deps.Lab.State(ctx)
	switch {
	case errors.Is(err, labstate.ErrToolchain):
		detail := err.Error()
		return GetLabState503ApplicationProblemPlusJSONResponse{Type: "/problems/toolchain",
			Title: "The lab toolchain failed", Status: http.StatusServiceUnavailable, Detail: &detail}, nil
	case err != nil:
		return nil, err
	}
	out := GetLabState200JSONResponse{Up: st.Up, Error: st.Error, Topology: st.Topology, Layout: layout(st.Layout),
		Fault: st.Fault, Sites: map[string]LabSite{}, Links: map[string]LabLink{}, Nodes: make([]LabNode, len(st.Nodes))}
	for name, site := range st.Sites {
		out.Sites[name] = LabSite{PublicIp: site.PublicIP, Vpc: site.VPC}
	}
	for name, l := range st.Links {
		out.Links[name] = LabLink{Live: linkConditions(l.Live), Desired: linkConditions(l.Desired),
			Condition: LabLinkCondition(l.Condition)}
	}
	for i, n := range st.Nodes {
		out.Nodes[i] = LabNode{Name: n.Name, Site: n.Site, Role: n.Role, Status: n.Status}
	}
	return out, nil
}

func linkConditions(c lab.LinkConditions) LinkConditions {
	out := LinkConditions{Rtt: c.RTT, Jitter: opt(c.Jitter), Loss: opt(c.Loss), Rate: opt(c.Rate)}
	if c.MTU != 0 {
		out.Mtu = &c.MTU
	}
	return out
}
