package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	catalogsqlite "github.com/IhebBelhadj/opsforge/academy/internal/catalog/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
	"github.com/IhebBelhadj/opsforge/academy/internal/history"
	historysqlite "github.com/IhebBelhadj/opsforge/academy/internal/history/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/identity"
	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
	labsqlite "github.com/IhebBelhadj/opsforge/academy/internal/labsession/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/labstate"
	"github.com/IhebBelhadj/opsforge/academy/internal/operations"
	operationssqlite "github.com/IhebBelhadj/opsforge/academy/internal/operations/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi/opsforgefake"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/progress"
	progresssqlite "github.com/IhebBelhadj/opsforge/academy/internal/progress/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/quiz"
	quizsqlite "github.com/IhebBelhadj/opsforge/academy/internal/quiz/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/rewards"
	rewardssqlite "github.com/IhebBelhadj/opsforge/academy/internal/rewards/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
	workspacesqlite "github.com/IhebBelhadj/opsforge/academy/internal/workspace/sqlite"
)

// world is the modules that keep learner state, on one temporary database (their tables refer to users) holding
// the golden bundle's catalogue, with the real event bus and hub.
type world struct {
	store    *sqlite.Store
	hub      *platform.Hub
	history  *history.Service
	catalog  *catalog.Service
	progress *progress.Service
	quizzes  *quiz.Service
	ops      *operations.Service
	spaces   *workspace.Service
	labs     *labsession.Service
	rewards  *rewards.Service
	state    *labstate.Service
	lab      *scriptedLab
	// OpsForge in memory: the workspaces and terminals (lab work is the scripted lab's)
	opsforge  *opsforgefake.Fake
	client    *opsforgeapi.Client
	terminals terminal.Shells
}

// scriptedLab is the toolchain as the HTTP tests see it: a lab that is up and passes every probe, unless told
// otherwise.
type scriptedLab struct {
	mu       sync.Mutex
	stateErr error
	failing  bool // probes fail
}

func (l *scriptedLab) State(context.Context) (lab.State, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stateErr != nil {
		return lab.State{}, l.stateErr
	}
	layout := "stretched"
	return lab.State{Up: true, Topology: "topology.yml", Layout: &layout,
		Sites: map[string]lab.Site{"a": {PublicIP: "203.0.113.10", VPC: "10.10.0.0/24"}},
		Links: map[string]lab.Link{"a-b": {Live: lab.LinkConditions{RTT: "40ms", Loss: "100%"},
			Desired: lab.LinkConditions{RTT: "40ms", Loss: "0.1%"}, Condition: "cut"}},
		Nodes:        []lab.Node{{Name: "a-cp1", Site: "a", Role: "k3s-server", Status: "Running"}},
		Fault:        true,
		Placeholders: map[string]string{"${node:c}": "c-w1", "${public:a}": "203.0.113.10"}}, nil
}

func (l *scriptedLab) Run(context.Context, lab.RunRequest) (lab.RunResult, error) {
	return lab.RunResult{Metrics: map[string]float64{}, Log: []string{}}, nil
}

func (l *scriptedLab) Probe(_ context.Context, req lab.ProbeRequest) (lab.ProbeResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	got := 0.0
	if l.failing {
		got = 1
	}
	return lab.ProbeResult{Passed: !l.failing, Results: []lab.Expectation{{Metric: "nodes.exit", Want: "== 0",
		Got: &got, Passed: !l.failing, Checked: true}}, Log: []string{}}, nil
}

func (l *scriptedLab) Heal(context.Context) (bool, error) { return false, nil }

// quizLab is a lab with a quiz step (the golden pack has none), added to the world's catalogue.
const quizLab = `{"id":"quiz-lab","title":"Quiz lab","xp":40,"files":{},"body":{"steps":[
{"id":"q","title":"Q","body":"?","hints":["think"],"check":{"quiz":{"question":"?","choices":["a","b"],"answer":1}}}]}}`

// wait lets the world's operations finish.
func (w *world) wait(t *testing.T) {
	t.Helper()
	if err := w.ops.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// inOrder is a Random that keeps every order (questions and choices as in the quiz file).
type inOrder struct{}

func (inOrder) Perm(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return p
}

func newWorld(t *testing.T) *world {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hub := platform.NewHub()
	t.Cleanup(hub.Close)
	data, err := os.ReadFile("../../testdata/bundle.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := contentimport.Entries(data)
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, catalog.Entry{Kind: catalog.KindLab, Key: "quiz-lab", PackID: "demo", Title: "Quiz lab",
		Hash: "QL", Doc: []byte(quizLab)})
	repo := catalogsqlite.NewRepo(store.DB)
	if err := repo.Apply(context.Background(), entries, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := platform.SystemClock{}
	w := &world{store: store, hub: hub, catalog: catalog.NewService(repo),
		history: history.NewService(historysqlite.NewRepo(store.DB), hub, clock, log)}
	w.quizzes = quiz.NewService(quizsqlite.NewRepo(store.DB), w.catalog, w.history, inOrder{}, clock)
	// As cmd/academy adapts them
	quizzes := progress.SourceFunc(func(ctx context.Context, user int64) ([]progress.Completion, error) {
		passed, err := w.quizzes.Passed(ctx, user)
		out := make([]progress.Completion, len(passed))
		for i, a := range passed {
			out[i] = progress.Completion{Type: catalog.ItemQuiz, Ref: a.Course + "/" + a.Item, Hash: a.Hash,
				At: *a.SubmittedAt}
		}
		return out, err
	})
	w.ops = operations.NewService(operationssqlite.NewRepo(store.DB), hub, clock, log)
	w.opsforge, w.client = fakeOpsForge(t)
	w.terminals, _ = shells(t, w.client)
	w.spaces = workspace.NewService(w.client.Files(), workspacesqlite.NewRepo(store.DB), clock)
	w.lab = &scriptedLab{}
	w.labs = labsession.NewService(labsqlite.NewRepo(store.DB), w.catalog, w.lab, w.spaces, w.ops, w.history, clock)
	t.Cleanup(func() { _ = w.ops.Wait(context.Background()) })
	labs := progress.SourceFunc(func(ctx context.Context, user int64) ([]progress.Completion, error) {
		passed, err := w.labs.Passed(ctx, user)
		out := make([]progress.Completion, len(passed))
		for i, a := range passed {
			out[i] = progress.Completion{Type: catalog.ItemLab, Ref: a.Lab, Hash: a.Hash, At: *a.FinishedAt}
		}
		return out, err
	})
	w.progress = progress.NewService(progresssqlite.NewRepo(store.DB), w.catalog, w.history, clock,
		progress.Sources{quizzes, labs})
	w.history.Subscribe(w.progress.OnEvent)
	ids := identity.NewService(identitysqlite.NewRepo(store.DB), clock)
	w.rewards = rewards.NewService(rewardssqlite.NewRepo(store.DB), w.catalog, rewardSources(w.labs, w.quizzes,
		w.progress, ids), w.history, clock, log)
	w.history.Subscribe(w.rewards.OnEvent)
	w.state = labstate.NewService(w.lab, clock)
	w.history.Subscribe(w.state.OnEvent)
	return w
}

// rewardSources reads the history for the rules from the modules, as cmd/academy adapts them.
func rewardSources(labs *labsession.Service, quizzes *quiz.Service, learning *progress.Service,
	ids *identity.Service) rewards.Sources {
	return rewards.Sources{
		Labs: func(ctx context.Context, user int64) ([]rewards.LabPass, error) {
			passed, err := labs.Passed(ctx, user)
			out := make([]rewards.LabPass, len(passed))
			for i, a := range passed {
				out[i] = rewards.LabPass{Lab: a.Lab, Hints: a.HintsUsed, Seconds: a.Seconds, XP: a.XP}
			}
			return out, err
		},
		Quizzes: func(ctx context.Context, user int64) ([]rewards.QuizPass, error) {
			passed, err := quizzes.Passed(ctx, user)
			out := make([]rewards.QuizPass, len(passed))
			for i, a := range passed {
				out[i] = rewards.QuizPass{Course: a.Course, Item: a.Item, XP: a.XP}
			}
			return out, err
		},
		Courses: learning.CompletedCourses,
		Users:   ids.UserIDs,
	}
}

// deps serves the world: identity on its database, the history and the event streams.
func (w *world) deps(d *httpapi.Deps) {
	d.Identity = identity.NewService(identitysqlite.NewRepo(w.store.DB), platform.SystemClock{})
	d.History = w.history
	d.Streams = w.hub
	d.Catalog = w.catalog
	d.Progress = w.progress
	d.Quizzes = w.quizzes
	d.Labs = w.labs
	d.Operations = w.ops
	d.Workspaces = w.spaces
	d.Awards = w.rewards
	d.Lab = w.state
	d.Terminals = w.terminals
}
