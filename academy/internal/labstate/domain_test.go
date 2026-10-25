package labstate_test

import (
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/labstate"
)

// values are the stretched layout's, as state.up.json has them.
var values = map[string]string{
	"${node:b}": "b-w1", "${server:b}": "a-cp1", "${cluster:b}": "lab", "${node:c}": "c-w1",
	"${ip:c-w1}": "10.30.0.11", "${ip:b-gw}": "10.20.0.1", "${public:a}": "203.0.113.10", "${ip:a-cp1}": "10.10.0.11",
}

func TestResolve(t *testing.T) {
	for text, want := range map[string]string{
		// the cases of tests/test_scenario.py
		"${node:b} ${server:b} ${cluster:b} ${ip:${node:c}}": "b-w1 a-cp1 lab 10.30.0.11",
		"http://${public:a}/ ${ip:a-cp1} ${ip:b-gw}":         "http://203.0.113.10/ 10.10.0.11 10.20.0.1",
		"no placeholders": "no placeholders",
		"ping ${public:zz} ${ip:${node:zz}} ${ns} ${unknown:a}": "ping ${public:zz} ${ip:${node:zz}} ${ns} ${unknown:a}",
	} {
		if got := labstate.Resolve(text, values); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", text, got, want)
		}
	}
	if got := labstate.Resolve("kubectl -n ${ns} get pods", map[string]string{"${ns}": "lab-7"}); got != "kubectl -n lab-7 get pods" {
		t.Errorf("${ns}: %q", got)
	}
	if got := labstate.Resolve("${node:b}", nil); got != "${node:b}" {
		t.Errorf("without values: %q", got)
	}
}
