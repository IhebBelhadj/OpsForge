package labsession_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
)

// TestPassXPMatchesPhase10 holds labs.pass_xp's values (python3 lab/labs.py), half-to-even rounding included
// (25 × 0.9 = 22.5 → 22).
func TestPassXPMatchesPhase10(t *testing.T) {
	for _, tc := range []struct {
		xp, hints int
		penalty   float64
		want      int
	}{
		{100, 0, 0.1, 100}, {100, 1, 0.1, 90}, {100, 5, 0.1, 50}, {100, 9, 0.1, 50}, {150, 1, 0.1, 135},
		{250, 3, 0.1, 175}, {25, 1, 0.1, 22}, {5, 1, 0.1, 4}, {200, 2, 0.25, 100}, {35, 1, 0.1, 32},
	} {
		if got := labsession.PassXP(tc.xp, tc.hints, tc.penalty); got != tc.want {
			t.Errorf("PassXP(%d, %d, %v) = %d, want %d", tc.xp, tc.hints, tc.penalty, got, tc.want)
		}
	}
}

func TestDecodeTheGoldenLab(t *testing.T) {
	data, err := os.ReadFile("../../testdata/bundle.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Packs []struct{ Labs []json.RawMessage }
	}
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	lab := catalog.Entry{Kind: catalog.KindLab, Key: "meet-the-cluster", Hash: "h", Doc: b.Packs[0].Labs[0]}
	def, err := labsession.Decode(lab, catalog.Entry{Doc: json.RawMessage(`{"id":"demo"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if def.XP != 100 || !def.Namespace || def.Reset != "setup" || def.HintPenalty != labsession.DefaultHintPenalty ||
		def.Files["hello.yaml"] == "" || len(def.Steps) != 2 {
		t.Fatalf("%+v", def)
	}
	probe := def.Steps[0].Check
	if probe.Type != labsession.CheckProbe || len(probe.Probe.Steps) != 1 || !strings.Contains(string(probe.Probe.Expect), `"nodes.exit"`) ||
		len(def.Steps[0].Hints) != 1 || def.Steps[1].Check.Type != labsession.CheckConfirm {
		t.Fatalf("%+v", def.Steps)
	}
	if _, err := labsession.Decode(catalog.Entry{Doc: json.RawMessage(`{"body":{"steps":[{"id":"x","check":{"magic":1}}]}}`)},
		catalog.Entry{Doc: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("an unknown check was accepted")
	}
}
