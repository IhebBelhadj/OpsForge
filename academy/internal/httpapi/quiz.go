package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/quiz"
)

// Quizzes is what the transport needs from the quiz module.
type Quizzes interface {
	Start(ctx context.Context, user int64, course, item string) (quiz.Attempt, error)
	Attempt(ctx context.Context, user, id int64) (quiz.Attempt, error)
	Submit(ctx context.Context, user, id int64, answers map[string][]int) (quiz.Attempt, error)
	Attempts(ctx context.Context, user int64, course, item string) ([]quiz.Attempt, error)
}

var notAQuiz = problem(http.StatusConflict, "/problems/not-a-quiz", "Not a quiz", "this item is not a quiz")

func attemptNotFound(what string) Problem {
	return problem(http.StatusNotFound, "/problems/not-found", "Not found", "no such "+what)
}

// StartQuizAttempt draws a new attempt at a quiz.
func (s *server) StartQuizAttempt(ctx context.Context, req StartQuizAttemptRequestObject) (StartQuizAttemptResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Quizzes.Start(ctx, user, req.Course, req.Item)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return StartQuizAttempt404ApplicationProblemPlusJSONResponse{itemNotFound(req.Course, req.Item)}, nil
	case errors.Is(err, quiz.ErrNotAQuiz):
		return StartQuizAttempt409ApplicationProblemPlusJSONResponse(notAQuiz), nil
	case err != nil:
		return nil, err
	}
	return StartQuizAttempt201JSONResponse(quizAttempt(a)), nil
}

// ListQuizAttempts is the learner's attempts at a quiz.
func (s *server) ListQuizAttempts(ctx context.Context, req ListQuizAttemptsRequestObject) (ListQuizAttemptsResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.deps.Quizzes.Attempts(ctx, user, req.Course, req.Item)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ListQuizAttempts404ApplicationProblemPlusJSONResponse{itemNotFound(req.Course, req.Item)}, nil
	case errors.Is(err, quiz.ErrNotAQuiz):
		return ListQuizAttempts409ApplicationProblemPlusJSONResponse(notAQuiz), nil
	case err != nil:
		return nil, err
	}
	out := ListQuizAttempts200JSONResponse{Attempts: make([]QuizAttemptSummary, len(list))}
	for i, a := range list {
		sum := QuizAttemptSummary{Id: a.ID, StartedAt: a.StartedAt, SubmittedAt: a.SubmittedAt, Xp: a.XP}
		if a.SubmittedAt != nil {
			sum.Score, sum.Passed = &a.Score, &a.Passed
		}
		out.Attempts[i] = sum
	}
	return out, nil
}

// GetQuizAttempt is one of the learner's attempts.
func (s *server) GetQuizAttempt(ctx context.Context, req GetQuizAttemptRequestObject) (GetQuizAttemptResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Quizzes.Attempt(ctx, user, req.Attempt)
	if errors.Is(err, quiz.ErrAttemptNotFound) {
		return GetQuizAttempt404ApplicationProblemPlusJSONResponse(attemptNotFound("quiz attempt")), nil
	}
	if err != nil {
		return nil, err
	}
	return GetQuizAttempt200JSONResponse(quizAttempt(a)), nil
}

// SubmitQuizAttempt scores an attempt.
func (s *server) SubmitQuizAttempt(ctx context.Context, req SubmitQuizAttemptRequestObject) (SubmitQuizAttemptResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Quizzes.Submit(ctx, user, req.Attempt, req.Body.Answers)
	switch {
	case errors.Is(err, quiz.ErrAttemptNotFound):
		return SubmitQuizAttempt404ApplicationProblemPlusJSONResponse(attemptNotFound("quiz attempt")), nil
	case errors.Is(err, quiz.ErrSubmitted):
		return SubmitQuizAttempt409ApplicationProblemPlusJSONResponse(problem(http.StatusConflict,
			"/problems/already-submitted", "Already submitted", "start a new attempt to try again")), nil
	case errors.Is(err, quiz.ErrExpired):
		return SubmitQuizAttempt409ApplicationProblemPlusJSONResponse(problem(http.StatusConflict,
			"/problems/quiz-expired", "Time is up", "the attempt's deadline has passed; start a new attempt")), nil
	case errors.Is(err, quiz.ErrInvalidAnswers):
		return SubmitQuizAttempt422ApplicationProblemPlusJSONResponse(problem(http.StatusUnprocessableEntity,
			"/problems/invalid-answers", "Answers do not fit the questions", err.Error())), nil
	case err != nil:
		return nil, err
	}
	return SubmitQuizAttempt200JSONResponse(quizAttempt(a)), nil
}

// quizAttempt is an attempt as the learner sees it: the questions without their keys, and the result (keys
// included) only once it is submitted.
func quizAttempt(a quiz.Attempt) QuizAttempt {
	out := QuizAttempt{Id: a.ID, Course: a.Course, Item: a.Item, StartedAt: a.StartedAt, Deadline: a.Deadline,
		SubmittedAt: a.SubmittedAt, Pass: a.Pass, Questions: make([]QuizQuestion, len(a.Questions))}
	for i, q := range a.Questions {
		choices := make([]QuizChoice, len(q.Choices))
		for j, c := range q.Choices {
			choices[j] = QuizChoice{Index: c.Index, Text: c.Text}
		}
		out.Questions[i] = QuizQuestion{Id: q.ID, Type: QuizQuestionType(q.Type), Prompt: q.Prompt, Choices: choices}
	}
	if results := a.Results(); results != nil {
		r := &QuizResult{Score: a.Score, Passed: a.Passed, Correct: a.Correct, Total: len(a.Questions), Xp: a.XP,
			Questions: make([]QuizQuestionResult, len(results))}
		for i, qr := range results {
			r.Questions[i] = QuizQuestionResult{Id: qr.ID, Correct: qr.Correct, Answer: qr.Answer, Given: qr.Given,
				Explain: qr.Explain, See: qr.See}
		}
		out.Result = r
	}
	return out
}
