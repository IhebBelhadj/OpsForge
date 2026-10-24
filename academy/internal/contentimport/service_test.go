package contentimport_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
)

type compiler struct {
	bundle json.RawMessage
	err    error
}

func (c *compiler) Compile(context.Context) (json.RawMessage, error) { return c.bundle, c.err }

// memStore is the catalogue in memory; applied counts the writes.
type memStore struct {
	mu      sync.Mutex
	entries []catalog.Entry
	at      map[string]time.Time
	applied int
}

func (s *memStore) Entries(context.Context) ([]catalog.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]catalog.Entry(nil), s.entries...), nil
}

func (s *memStore) Apply(_ context.Context, upsert, retire []catalog.Entry, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied++
	index := map[string]int{}
	for i, e := range s.entries {
		index[string(e.Kind)+" "+e.Key] = i
	}
	for _, e := range upsert {
		e.Retired = false
		if i, ok := index[string(e.Kind)+" "+e.Key]; ok {
			s.entries[i] = e
		} else {
			s.entries = append(s.entries, e)
		}
		s.at[string(e.Kind)+" "+e.Key] = at
	}
	for _, e := range retire {
		s.entries[index[string(e.Kind)+" "+e.Key]].Retired = true
		s.at[string(e.Kind)+" "+e.Key] = at
	}
	return nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func TestImportingTheSamePacksTwiceChangesNothing(t *testing.T) {
	store := &memStore{at: map[string]time.Time{}}
	c := &clock{now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	svc := contentimport.NewService(&compiler{bundle: golden(t)}, store, c)

	first, err := svc.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || first.Added[catalog.KindItem] != 4 || first.Added[catalog.KindLab] != 1 || store.applied != 1 {
		t.Fatalf("first import: %+v", first)
	}
	c.now = c.now.Add(time.Hour)
	second, err := svc.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || second.Unchanged[catalog.KindItem] != 4 || store.applied != 1 {
		t.Fatalf("second import: %+v (applied %d)", second, store.applied)
	}
	if !store.at["pack demo"].Equal(c.now.Add(-time.Hour)) {
		t.Fatalf("the pack's import time moved: %v", store.at["pack demo"])
	}
}

func TestRemovedContentIsRetired(t *testing.T) {
	store := &memStore{at: map[string]time.Time{}}
	comp := &compiler{bundle: golden(t)}
	svc := contentimport.NewService(comp, store, &clock{now: time.Now()})
	if _, err := svc.Import(context.Background()); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(golden(t), &doc)
	doc["packs"] = []any{}
	comp.bundle, _ = json.Marshal(doc)
	report, err := svc.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Retired[catalog.KindLab] != 1 || report.Retired[catalog.KindPack] != 1 || len(store.entries) != 11 {
		t.Fatalf("report %+v, %d entries", report, len(store.entries))
	}
	for _, e := range store.entries {
		if !e.Retired {
			t.Errorf("%s %s still live", e.Kind, e.Key)
		}
	}
}

func TestAFailedCompileWritesNothing(t *testing.T) {
	store := &memStore{at: map[string]time.Time{}}
	var doc map[string]any
	_ = json.Unmarshal(golden(t), &doc)
	doc["errors"] = []any{map[string]any{"pack": "demo", "message": "broken"}}
	withErrors, _ := json.Marshal(doc)
	boom := errors.New("toolchain down")
	for name, comp := range map[string]*compiler{
		"bundle errors": {bundle: withErrors},
		"toolchain":     {err: boom},
	} {
		_, err := contentimport.NewService(comp, store, &clock{now: time.Now()}).Import(context.Background())
		var be *contentimport.BundleErrors
		if err == nil || (name == "bundle errors" && !errors.As(err, &be)) || (name == "toolchain" &&
			(!errors.Is(err, boom) || !errors.Is(err, contentimport.ErrCompile))) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if store.applied != 0 {
		t.Fatalf("%d writes", store.applied)
	}
}
