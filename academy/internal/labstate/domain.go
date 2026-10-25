// Package labstate is the lab as the pages see it (docs/courses/architecture.md, "Platform service: modules"): the
// state the toolchain reports (instances, links against topology.yml, faults), cached briefly because a call costs
// an Incus round trip, and the lab texts with their placeholders resolved for the running topology. The values come
// from the toolchain (`state`'s placeholders, computed by the same scenario.substitute that runs the checks), so
// what a learner reads is what the probes use.
package labstate

import (
	"errors"
	"regexp"
	"time"
)

// ErrToolchain wraps a state the toolchain could not give (not installed, crashed, another contract).
var ErrToolchain = errors.New("the lab toolchain failed")

// CacheTTL is how long a state is served before the toolchain is asked again.
const CacheTTL = 5 * time.Second

// The two passes of scenario.substitute: ${node|server|cluster:<site>} first, so ${ip:${node:a}} resolves.
var (
	nodePlaceholder  = regexp.MustCompile(`\$\{(?:node|server|cluster):[a-z0-9-]+\}`)
	valuePlaceholder = regexp.MustCompile(`\$\{(?:ip|public):[a-z0-9-]+\}|\$\{ns\}`)
)

// Resolve replaces the placeholders of text found in values (keys as written: "${node:a}"); the others stay as
// written (a site the topology lacks, ${ns} outside an attempt).
func Resolve(text string, values map[string]string) string {
	if len(values) == 0 {
		return text
	}
	lookup := func(m string) string {
		if v, ok := values[m]; ok {
			return v
		}
		return m
	}
	return valuePlaceholder.ReplaceAllStringFunc(nodePlaceholder.ReplaceAllStringFunc(text, lookup), lookup)
}
