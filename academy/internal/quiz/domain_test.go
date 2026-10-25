package quiz_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/quiz"
)

// reverse is a Random whose permutations are n-1..0.
type reverse struct{}

func (reverse) Perm(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = n - 1 - i
	}
	return p
}

// goldenQuiz is pods-check of the golden bundle (a single and a multiple question).
func goldenQuiz(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("../../testdata/bundle.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var b struct {
		Packs []struct{ Quizzes []json.RawMessage }
	}
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	return b.Packs[0].Quizzes[0]
}

func TestDecodeKeepsTheKeys(t *testing.T) {
	def, err := quiz.Decode(goldenQuiz(t))
	if err != nil {
		t.Fatal(err)
	}
	if def.Pass != 70 || def.XP != 20 || !def.Shuffle || len(def.Questions) != 2 {
		t.Fatalf("%+v", def)
	}
	if q := def.Questions[1]; q.ID != "phases" || q.Type != quiz.Multiple || len(q.Answer) != 3 || q.Explain == "" {
		t.Fatalf("%+v", q)
	}
	if _, err := quiz.Decode(json.RawMessage(`[]`)); err == nil {
		t.Fatal("not a quiz document")
	}
}

func def(shuffle bool, draw *int) quiz.Definition {
	return quiz.Definition{Pass: 50, Shuffle: shuffle, Draw: draw, Questions: []quiz.Question{
		{ID: "a", Type: quiz.Single, Choices: []string{"a0", "a1"}, Answer: []int{1}},
		{ID: "b", Type: quiz.Multiple, Choices: []string{"b0", "b1", "b2"}, Answer: []int{0, 2}},
		{ID: "c", Type: quiz.Single, Choices: []string{"c0", "c1"}, Answer: []int{0}},
	}}
}

func ids(qs []quiz.Drawn) string {
	s := ""
	for _, q := range qs {
		s += q.ID
	}
	return s
}

func TestDraw(t *testing.T) {
	two := 2
	for name, tc := range map[string]struct {
		def     quiz.Definition
		want    string
		choices string // of question b, by index
	}{
		"in order":                {def(false, nil), "abc", "012"},
		"shuffled":                {def(true, nil), "cba", "210"},
		"drawn, kept in order":    {def(false, &two), "bc", "012"},
		"drawn and shuffled":      {def(true, &two), "cb", "210"},
		"draw above the question": {def(false, new(int)), "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := quiz.Draw(tc.def, reverse{})
			if ids(got) != tc.want {
				t.Fatalf("questions %s, want %s", ids(got), tc.want)
			}
			for _, q := range got {
				if q.ID != "b" {
					continue
				}
				s := ""
				for _, c := range q.Choices {
					s += string(rune('0' + c.Index))
					if c.Text != "b"+string(rune('0'+c.Index)) {
						t.Fatalf("choice %+v lost its text", c)
					}
				}
				if s != tc.choices {
					t.Fatalf("choices %s, want %s", s, tc.choices)
				}
			}
		})
	}
	five := 5
	if got := quiz.Draw(def(false, &five), reverse{}); ids(got) != "abc" {
		t.Fatalf("draw over the count: %s", ids(got))
	}
}

func TestGrade(t *testing.T) {
	for name, tc := range map[string]struct {
		answers map[string][]int
		correct int
		score   int
		passed  bool
		err     error
	}{
		"all right":                   {map[string][]int{"a": {1}, "b": {2, 0}, "c": {0}}, 3, 100, true, nil},
		"multiple is all or nothing":  {map[string][]int{"a": {1}, "b": {0}, "c": {1}}, 1, 33, false, nil},
		"repeats count once":          {map[string][]int{"a": {1, 1}, "b": {0, 2, 0}}, 2, 66, true, nil},
		"left out is wrong":           {map[string][]int{"a": {1}, "b": {0, 2}}, 2, 66, true, nil},
		"empty is wrong":              {map[string][]int{"a": {}}, 0, 0, false, nil},
		"unknown question":            {map[string][]int{"z": {0}}, 0, 0, false, quiz.ErrInvalidAnswers},
		"no such choice":              {map[string][]int{"a": {2}}, 0, 0, false, quiz.ErrInvalidAnswers},
		"two answers to a single one": {map[string][]int{"c": {0, 1}}, 0, 0, false, quiz.ErrInvalidAnswers},
	} {
		t.Run(name, func(t *testing.T) {
			a := quiz.Attempt{Questions: quiz.Draw(def(true, nil), reverse{}), Pass: 50}
			results, err := a.Grade(tc.answers, time.Now())
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				if a.SubmittedAt != nil {
					t.Fatal("a refused submission was graded")
				}
				return
			}
			if a.Correct != tc.correct || a.Score != tc.score || a.Passed != tc.passed || len(results) != 3 {
				t.Fatalf("correct %d score %d passed %v", a.Correct, a.Score, a.Passed)
			}
			if again := a.Results(); len(again) != 3 || again[0].Correct != results[0].Correct {
				t.Fatalf("results of the stored attempt %+v", again)
			}
		})
	}
}

func TestResultsComeOnlyAfterSubmitting(t *testing.T) {
	a := quiz.Attempt{Questions: quiz.Draw(def(false, nil), reverse{})}
	if a.Results() != nil {
		t.Fatal("results before submitting")
	}
}
