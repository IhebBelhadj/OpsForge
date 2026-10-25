// Package catalog is the content as imported: packs, courses with their items, lectures, quizzes, challenges, labs
// and badges (docs/courses/content-model.md). Pack files are the source of truth; the catalogue mirrors them
// (contentimport) and never deletes an entry: removed content is retired, as attempts and awards refer to it.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Kind is a type of entry.
type Kind string

// The kinds of entries. Keys: pack, course, lab and badge ids; "<course>/<id>" for items, lectures, quizzes and
// challenges (their ids are unique within a course).
const (
	KindPack      Kind = "pack"
	KindCourse    Kind = "course"
	KindItem      Kind = "item"
	KindLecture   Kind = "lecture"
	KindQuiz      Kind = "quiz"
	KindChallenge Kind = "challenge"
	KindLab       Kind = "lab"
	KindBadge     Kind = "badge"
)

// Entry is one piece of content: Doc is its bundle document (compact JSON: a pack's without the lists of its
// entries, a course's with its sections). Hash is its content hash, which a completion records to tell when the
// content changed since; Position orders it among its siblings (courses and badges in their pack, items in their
// course).
type Entry struct {
	Kind     Kind
	Key      string
	PackID   string
	CourseID string // "" for entries outside a course
	Position int
	Title    string
	Hash     string
	Doc      json.RawMessage
	Retired  bool
}

// Same reports whether e and o hold the same content at the same place (retirement aside).
func (e Entry) Same(o Entry) bool {
	return e.Kind == o.Kind && e.Key == o.Key && e.PackID == o.PackID && e.CourseID == o.CourseID &&
		e.Position == o.Position && e.Title == o.Title && e.Hash == o.Hash && string(e.Doc) == string(o.Doc)
}

// ErrNotFound is an entry that is not in the catalogue, or is retired.
var ErrNotFound = errors.New("not in the catalogue")

// The views below are what the learner may see of the content. They are decoded from the entries' documents into
// types that have no field for what is earned (quiz keys and explanations, reference solutions, how checks and
// tests run, hint texts), so nothing earned can be shown by mistake.

// CourseSummary is a course in the catalogue list.
type CourseSummary struct {
	ID, Pack, Title, Subtitle, Level string
	Minutes                          int
	Needs, Requires                  []string
	Layout, Badge                    *string
	Items                            int // in all its sections
	Sections                         int
	Counts                           ItemCounts
}

// ItemCounts are a course's items per type.
type ItemCounts struct{ Lecture, Quiz, Lab, Challenge int }

func (c *ItemCounts) add(typ string) {
	switch typ {
	case ItemLecture:
		c.Lecture++
	case ItemQuiz:
		c.Quiz++
	case ItemLab:
		c.Lab++
	case ItemChallenge:
		c.Challenge++
	}
}

// Course is the course page: its summary, texts and sections.
type Course struct {
	CourseSummary
	Description            string
	Outcomes, Requirements []string
	Hash                   string
	Sections               []Section
}

// Section is a course section with its items in order.
type Section struct {
	Title   string
	Layout  *string
	Needs   []string
	Minutes int
	Items   []ItemRef
}

// ItemRef is an item as its course lists it.
type ItemRef struct {
	ID, Type, Title string
	Minutes         int
	Hash            string
}

// Item types.
const (
	ItemLecture   = "lecture"
	ItemQuiz      = "quiz"
	ItemLab       = "lab"
	ItemChallenge = "challenge"
)

// Item is one item of a course with the content of its type (exactly one of Lecture, Quiz, Lab, Challenge).
type Item struct {
	Course, ID, Type, Title string
	Minutes, Section        int
	Hash                    string
	Lecture                 *Lecture
	Quiz                    *Quiz
	Lab                     *Lab
	Challenge               *Challenge
}

// Lecture is a lecture's text.
type Lecture struct {
	Markdown  string
	Resources []Resource
}

// Resource is a link a lecture recommends.
type Resource struct{ Title, URL string }

// Quiz is what an attempt will be; the questions come with the attempt (drawn, with its deadline).
type Quiz struct {
	Pass, XP   int
	TimeLimitS *int
	Questions  int // per attempt
}

// Challenge is the statement and the starting files; the tests are named, not shown.
type Challenge struct {
	Difficulty string
	XP         int
	Needs      []string
	Statement  string
	Files      []string
	Starter    map[string]string
	TimeLimitS int
	Tests      []string
	Hints      int // how many there are; each is opened at a cost
}

// Lab is a lab's texts and steps; setup, teardown and the checks' probes stay on the server.
type Lab struct {
	ID, Summary              string
	XP                       int
	Needs, Requires          []string
	Layout                   *string
	Briefing, Debrief        string
	Concepts, ClusterChanges []string
	Namespace                bool // the lab gets its own namespace (${ns})
	Files                    []string
	Steps                    []LabStep
}

// LabStep is one step: its text and the kind of check that passes it.
type LabStep struct {
	ID, Title, Body, Open string
	Check                 Check
	Hints                 int
}

// Check is how a step is passed: "probe" (the platform observes the lab), "confirm" (the learner says so, Label
// on the button) or "quiz" (Question and Choices; the answer stays on the server).
type Check struct {
	Type, Label, Question string
	Choices               []string
}

// CourseView decodes a course entry.
func CourseView(e Entry) (Course, error) {
	var doc struct {
		Title, Subtitle, Level, Description string
		Minutes                             int
		Needs, Requires                     []string
		Outcomes, Requirements              []string
		Layout, Badge                       *string
		Sections                            []struct {
			Title   string
			Layout  *string
			Needs   []string
			Minutes int
			Items   []struct {
				ID, Type, Title string
				Minutes         int
				ContentHash     string `json:"content_hash"`
			}
		}
	}
	if err := json.Unmarshal(e.Doc, &doc); err != nil {
		return Course{}, fmt.Errorf("course %s: %w", e.Key, err)
	}
	c := Course{CourseSummary: CourseSummary{ID: e.Key, Pack: e.PackID, Title: doc.Title, Subtitle: doc.Subtitle,
		Level: doc.Level, Minutes: doc.Minutes, Needs: doc.Needs, Requires: doc.Requires, Layout: doc.Layout,
		Badge: doc.Badge}, Description: doc.Description, Outcomes: doc.Outcomes, Requirements: doc.Requirements,
		Hash: e.Hash}
	for _, s := range doc.Sections {
		sec := Section{Title: s.Title, Layout: s.Layout, Needs: s.Needs, Minutes: s.Minutes}
		for _, it := range s.Items {
			sec.Items = append(sec.Items, ItemRef{ID: it.ID, Type: it.Type, Title: it.Title, Minutes: it.Minutes,
				Hash: it.ContentHash})
			c.Counts.add(it.Type)
		}
		c.Items += len(sec.Items)
		c.Sections = append(c.Sections, sec)
	}
	c.CourseSummary.Sections = len(c.Sections)
	return c, nil
}

// itemDoc is an item entry's document: its course entry and section; Ref names its lecture, quiz, challenge
// (in the course) or lab (in the pack).
type itemDoc struct {
	ID, Type, Ref, Title string
	Minutes, Section     int
}

func decodeItem(e Entry) (itemDoc, error) {
	var d itemDoc
	if err := json.Unmarshal(e.Doc, &d); err != nil {
		return d, fmt.Errorf("item %s: %w", e.Key, err)
	}
	return d, nil
}

// LectureView decodes a lecture entry.
func LectureView(e Entry) (*Lecture, error) {
	var doc struct {
		Markdown  string
		Resources []struct{ Title, URL string }
	}
	if err := json.Unmarshal(e.Doc, &doc); err != nil {
		return nil, fmt.Errorf("lecture %s: %w", e.Key, err)
	}
	l := &Lecture{Markdown: doc.Markdown}
	for _, r := range doc.Resources {
		l.Resources = append(l.Resources, Resource(r))
	}
	return l, nil
}

// QuizView decodes a quiz entry: how many questions an attempt has, not which.
func QuizView(e Entry) (*Quiz, error) {
	var doc struct {
		Pass, XP   int
		TimeLimitS *int `json:"time_limit_s"`
		Draw       *int
		Questions  []json.RawMessage
	}
	if err := json.Unmarshal(e.Doc, &doc); err != nil {
		return nil, fmt.Errorf("quiz %s: %w", e.Key, err)
	}
	n := len(doc.Questions)
	if doc.Draw != nil && *doc.Draw < n {
		n = *doc.Draw
	}
	return &Quiz{Pass: doc.Pass, XP: doc.XP, TimeLimitS: doc.TimeLimitS, Questions: n}, nil
}

// ChallengeView decodes a challenge entry without its solution, its tests' probes or its hint texts.
func ChallengeView(e Entry) (*Challenge, error) {
	var doc struct {
		Difficulty, Statement string
		XP                    int
		Needs, Files, Hints   []string
		Starter               map[string]string
		TimeLimitS            int `json:"time_limit_s"`
		Tests                 []struct{ Name string }
	}
	if err := json.Unmarshal(e.Doc, &doc); err != nil {
		return nil, fmt.Errorf("challenge %s: %w", e.Key, err)
	}
	c := &Challenge{Difficulty: doc.Difficulty, XP: doc.XP, Needs: doc.Needs, Statement: doc.Statement,
		Files: doc.Files, Starter: doc.Starter, TimeLimitS: doc.TimeLimitS, Hints: len(doc.Hints)}
	for _, t := range doc.Tests {
		c.Tests = append(c.Tests, t.Name)
	}
	return c, nil
}

// LabView decodes a lab entry without its setup, teardown, probes, quiz answers or hint texts.
func LabView(e Entry) (*Lab, error) {
	var doc struct {
		Summary  string
		XP       int
		Needs    []string
		Requires []string
		Layout   *string
		Files    map[string]string
		Body     struct {
			Briefing, Debrief, Namespace string
			Concepts                     []string
			ClusterChanges               []string `json:"cluster_changes"`
			Steps                        []struct {
				ID, Title, Body, Open string
				Hints                 []string
				Check                 map[string]json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(e.Doc, &doc); err != nil {
		return nil, fmt.Errorf("lab %s: %w", e.Key, err)
	}
	l := &Lab{ID: e.Key, Summary: doc.Summary, XP: doc.XP, Needs: doc.Needs, Requires: doc.Requires,
		Layout: doc.Layout, Briefing: doc.Body.Briefing, Debrief: doc.Body.Debrief, Concepts: doc.Body.Concepts,
		ClusterChanges: doc.Body.ClusterChanges, Namespace: doc.Body.Namespace == "auto"}
	for path := range doc.Files {
		l.Files = append(l.Files, path)
	}
	sort.Strings(l.Files)
	for _, s := range doc.Body.Steps {
		check, err := checkView(s.Check)
		if err != nil {
			return nil, fmt.Errorf("lab %s step %s: %w", e.Key, s.ID, err)
		}
		l.Steps = append(l.Steps, LabStep{ID: s.ID, Title: s.Title, Body: s.Body, Open: s.Open, Check: check,
			Hints: len(s.Hints)})
	}
	return l, nil
}

// checkView is a step's check (one key: its type) as the learner sees it.
func checkView(check map[string]json.RawMessage) (Check, error) {
	for kind, arg := range check {
		c := Check{Type: kind}
		switch kind {
		case "confirm":
			if err := json.Unmarshal(arg, &c.Label); err != nil {
				return c, err
			}
		case "quiz":
			var q struct {
				Question string
				Choices  []json.RawMessage
			}
			if err := json.Unmarshal(arg, &q); err != nil {
				return c, err
			}
			c.Question = q.Question
			for _, choice := range q.Choices { // YAML may give a number; it is shown as written
				var s string
				if json.Unmarshal(choice, &s) != nil {
					s = string(choice)
				}
				c.Choices = append(c.Choices, s)
			}
		}
		return c, nil
	}
	return Check{}, errors.New("a step without a check")
}
