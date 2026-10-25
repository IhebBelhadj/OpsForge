// Package quiz runs multiple-choice quizzes (docs/courses/content-model.md, "Quiz"): an attempt draws its
// questions (shuffled unless the quiz says not to, `draw` of them for a final exam) with a deadline when the quiz
// has a time limit, and is scored on the server. Keys and explanations never leave it before the attempt is
// submitted; the attempt keeps the questions as drawn, so editing the quiz does not change a running attempt.
package quiz

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// KindSubmitted is the event of a submitted attempt (subject "<course>/<item>").
const KindSubmitted = "quiz.submitted"

// DeadlineGrace is how late a submission may arrive after the deadline (the request's own travel time).
const DeadlineGrace = 5 * time.Second

// Question types.
const (
	Single   = "single"
	Multiple = "multiple" // all or nothing
)

var (
	// ErrNotAQuiz is starting a quiz attempt on another type of item.
	ErrNotAQuiz = errors.New("the item is not a quiz")
	// ErrAttemptNotFound is an attempt that does not exist or is another user's.
	ErrAttemptNotFound = errors.New("no such quiz attempt")
	// ErrSubmitted is submitting an attempt twice.
	ErrSubmitted = errors.New("the attempt is already submitted")
	// ErrExpired is a submission after the attempt's deadline.
	ErrExpired = errors.New("the attempt's time is up")
	// ErrInvalidAnswers is an answer to a question the attempt does not have, or a choice it does not offer.
	ErrInvalidAnswers = errors.New("the answers do not fit the questions")
)

// Definition is a quiz as imported, keys included.
type Definition struct {
	Pass, XP   int
	Shuffle    bool
	TimeLimitS *int
	Draw       *int
	Questions  []Question
}

// Question is one question with its key.
type Question struct {
	ID, Type, Prompt string
	Choices          []string
	Answer           []int
	Explain          string
	See              *string
}

// Decode reads a quiz entry's bundle document.
func Decode(doc json.RawMessage) (Definition, error) {
	var d struct {
		Pass, XP   int
		Shuffle    bool
		TimeLimitS *int `json:"time_limit_s"`
		Draw       *int
		Questions  []struct {
			ID, Type, Prompt, Explain string
			Choices                   []json.RawMessage
			Answer                    []int
			See                       *string
		}
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return Definition{}, fmt.Errorf("quiz: %w", err)
	}
	def := Definition{Pass: d.Pass, XP: d.XP, Shuffle: d.Shuffle, TimeLimitS: d.TimeLimitS, Draw: d.Draw}
	for _, q := range d.Questions {
		question := Question{ID: q.ID, Type: q.Type, Prompt: q.Prompt, Answer: q.Answer, Explain: q.Explain, See: q.See}
		for _, c := range q.Choices { // YAML may give a number; it is shown as written
			var s string
			if json.Unmarshal(c, &s) != nil {
				s = string(c)
			}
			question.Choices = append(question.Choices, s)
		}
		def.Questions = append(def.Questions, question)
	}
	return def, nil
}

// Choice is a choice as an attempt shows it: Index is its place in the quiz file, which answers name.
type Choice struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// Drawn is a question as an attempt holds it: in the attempt's order, choices in the attempt's order, with its key.
type Drawn struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Prompt  string   `json:"prompt"`
	Choices []Choice `json:"choices"`
	Answer  []int    `json:"answer"`
	Explain string   `json:"explain"`
	See     *string  `json:"see"`
}

// Draw picks an attempt's questions: `draw` of them at random when set, in a random order with shuffled choices
// when the quiz shuffles (drawn questions keep the file's order otherwise).
func Draw(def Definition, r platform.Random) []Drawn {
	order := make([]int, len(def.Questions))
	for i := range order {
		order[i] = i
	}
	if def.Draw != nil && *def.Draw < len(order) {
		order = r.Perm(len(order))[:*def.Draw]
		if !def.Shuffle {
			slices.Sort(order)
		}
	} else if def.Shuffle {
		order = r.Perm(len(order))
	}
	out := make([]Drawn, len(order))
	for i, qi := range order {
		q := def.Questions[qi]
		choices := make([]int, len(q.Choices))
		for j := range choices {
			choices[j] = j
		}
		if def.Shuffle {
			choices = r.Perm(len(choices))
		}
		d := Drawn{ID: q.ID, Type: q.Type, Prompt: q.Prompt, Answer: q.Answer, Explain: q.Explain, See: q.See}
		for _, ci := range choices {
			d.Choices = append(d.Choices, Choice{Index: ci, Text: q.Choices[ci]})
		}
		out[i] = d
	}
	return out
}

// Result is one question's verdict: the key and what was given, as choice indexes in the file's order.
type Result struct {
	ID      string
	Correct bool
	Answer  []int
	Given   []int
	Explain string
	See     *string
}

// Score grades answers (question id → chosen indexes; a question left out is wrong) against the drawn questions.
func Score(questions []Drawn, answers map[string][]int) ([]Result, int, error) {
	byID := make(map[string]Drawn, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
	}
	for id, given := range answers {
		q, ok := byID[id]
		if !ok || (q.Type == Single && len(normalize(given)) > 1) {
			return nil, 0, fmt.Errorf("%w: question %q", ErrInvalidAnswers, id)
		}
		for _, c := range given {
			if c < 0 || c >= len(q.Choices) {
				return nil, 0, fmt.Errorf("%w: question %q has no choice %d", ErrInvalidAnswers, id, c)
			}
		}
	}
	results := make([]Result, len(questions))
	correct := 0
	for i, q := range questions {
		given := normalize(answers[q.ID])
		r := Result{ID: q.ID, Answer: normalize(q.Answer), Given: given, Explain: q.Explain, See: q.See}
		r.Correct = len(given) > 0 && slices.Equal(given, r.Answer)
		if r.Correct {
			correct++
		}
		results[i] = r
	}
	return results, correct, nil
}

// normalize is a set of choice indexes in order, without repeats, never nil.
func normalize(s []int) []int {
	out := slices.Clone(s)
	slices.Sort(out)
	out = slices.Compact(out)
	if out == nil {
		out = []int{}
	}
	return out
}

// Attempt is one try at a quiz. Pass and XP are the quiz's when it started; Score is a percentage (rounded down);
// XP what this attempt earned (the quiz's xp on its first pass, 0 otherwise).
type Attempt struct {
	ID          int64
	UserID      int64
	Course      string
	Item        string
	Hash        string
	StartedAt   time.Time
	Deadline    *time.Time
	SubmittedAt *time.Time
	Questions   []Drawn
	Pass        int
	Offered     int // the quiz's xp
	Answers     map[string][]int
	Correct     int
	Score       int
	Passed      bool
	XP          int
}

// Grade fills in the verdict for answers submitted at `at`.
func (a *Attempt) Grade(answers map[string][]int, at time.Time) ([]Result, error) {
	results, correct, err := Score(a.Questions, answers)
	if err != nil {
		return nil, err
	}
	a.Answers, a.Correct, a.SubmittedAt = answers, correct, &at
	if n := len(a.Questions); n > 0 {
		a.Score = correct * 100 / n
		a.Passed = correct*100 >= a.Pass*n
	}
	return results, nil
}

// Results are a submitted attempt's verdicts (nil before it is submitted).
func (a Attempt) Results() []Result {
	if a.SubmittedAt == nil {
		return nil
	}
	results, _, _ := Score(a.Questions, a.Answers) // graded once already
	return results
}
