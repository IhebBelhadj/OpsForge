package progress_test

import (
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/progress"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func at(min int) *time.Time {
	t := t0.Add(time.Duration(min) * time.Minute)
	return &t
}

// course is k8s-intro of the golden bundle in two sections (one item of each type, the lab under its own id).
func course() catalog.Course {
	return catalog.Course{CourseSummary: catalog.CourseSummary{ID: "k8s-intro"}, Sections: []catalog.Section{
		{Items: []catalog.ItemRef{
			{ID: "what-is-a-pod", Type: "lecture", Ref: "what-is-a-pod", Hash: "L2"},
			{ID: "pods-check", Type: "quiz", Ref: "pods-check", Hash: "Q1"},
		}},
		{Items: []catalog.ItemRef{
			{ID: "meet", Type: "lab", Ref: "meet-the-cluster", Hash: "B1"},
			{ID: "first-pod", Type: "challenge", Ref: "first-pod", Hash: "C1"},
		}},
	}}
}

func statuses(p progress.CourseProgress) string {
	s := ""
	for _, it := range p.Items {
		s += string(it.Status[0])
		if it.Stale {
			s += "*"
		}
	}
	return s
}

func TestCompute(t *testing.T) {
	for name, tc := range map[string]struct {
		records  map[string]progress.Record
		done     []progress.Completion
		want     string // n(ot started), i(n progress), c(ompleted), * stale
		complete bool
		resume   string
	}{
		"nothing yet": {want: "nnnn", resume: "what-is-a-pod"},
		"opened the quiz": {
			records: map[string]progress.Record{"pods-check": {OpenedAt: at(1)}},
			want:    "ninn", resume: "pods-check",
		},
		"lecture completed on old content": {
			records: map[string]progress.Record{"what-is-a-pod": {Hash: "L1", CompletedAt: at(1), OpenedAt: at(0)}},
			want:    "c*nnn", resume: "pods-check",
		},
		"a lab passed in another course, keyed by the lab": {
			done: []progress.Completion{{Type: "lab", Ref: "meet-the-cluster", Hash: "B1", At: *at(2)}},
			want: "nncn", resume: "what-is-a-pod",
		},
		"the quiz passed twice, the latest on new content": {
			done: []progress.Completion{
				{Type: "quiz", Ref: "k8s-intro/pods-check", Hash: "Q1", At: *at(5)},
				{Type: "quiz", Ref: "k8s-intro/pods-check", Hash: "Q0", At: *at(3)},
				{Type: "quiz", Ref: "other/pods-check", Hash: "Q1", At: *at(3)},
			},
			want: "ncnn", resume: "what-is-a-pod",
		},
		"resume after the last opened, wrapping round": {
			records: map[string]progress.Record{
				"what-is-a-pod": {OpenedAt: at(0)},
				"first-pod":     {OpenedAt: at(9), Hash: "C1"},
			},
			done: []progress.Completion{{Type: "challenge", Ref: "k8s-intro/first-pod", Hash: "C1", At: *at(9)}},
			want: "innc", resume: "what-is-a-pod",
		},
		"everything": {
			records: map[string]progress.Record{"what-is-a-pod": {Hash: "L2", CompletedAt: at(1)}},
			done: []progress.Completion{
				{Type: "quiz", Ref: "k8s-intro/pods-check", Hash: "Q1", At: *at(2)},
				{Type: "lab", Ref: "meet-the-cluster", Hash: "B0", At: *at(3)},
				{Type: "challenge", Ref: "k8s-intro/first-pod", Hash: "C1", At: *at(4)},
			},
			want: "ccc*c", complete: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := progress.Compute(course(), tc.records, progress.Completions(tc.done), nil)
			if got := statuses(p); got != tc.want || p.Complete != tc.complete || p.Resume != tc.resume {
				t.Fatalf("statuses %s complete %v resume %q; want %s %v %q", got, p.Complete, p.Resume, tc.want,
					tc.complete, tc.resume)
			}
			if p.Total != 4 || p.Completed != len(p.Items)-countNot(p) {
				t.Fatalf("counts %d/%d", p.Completed, p.Total)
			}
		})
	}
}

func countNot(p progress.CourseProgress) int {
	n := 0
	for _, it := range p.Items {
		if it.Status != progress.Completed {
			n++
		}
	}
	return n
}

func TestAnEmptyCourseIsNeverComplete(t *testing.T) {
	p := progress.Compute(catalog.Course{CourseSummary: catalog.CourseSummary{ID: "empty"}}, nil, nil, nil)
	if p.Complete || p.Resume != "" || p.Total != 0 {
		t.Fatalf("%+v", p)
	}
}
