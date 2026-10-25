package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
	"github.com/IhebBelhadj/opsforge/academy/internal/operations"
)

// Labs is what the transport needs from the labsession module.
type Labs interface {
	Start(ctx context.Context, user int64, lab, course, item string) (labsession.Attempt, operations.Operation, error)
	Current(ctx context.Context, user int64) (labsession.View, bool, error)
	View(ctx context.Context, user, id int64) (labsession.View, error)
	Check(ctx context.Context, user, id int64, step string, answer *int) (operations.Operation, error)
	Hint(ctx context.Context, user, id int64, step string) (labsession.HintShown, error)
	Reset(ctx context.Context, user, id int64) (operations.Operation, error)
	Abandon(ctx context.Context, user, id int64) (operations.Operation, error)
}

// Operations is what the transport needs from the operations module.
type Operations interface {
	Get(ctx context.Context, user int64, id string) (operations.Operation, error)
}

// labProblem maps a labsession error to its problem document (status 0: not a domain error).
func labProblem(err error) Problem {
	var locked *labsession.LockedError
	switch {
	case errors.Is(err, labsession.ErrLabNotFound):
		return problem(http.StatusNotFound, "/problems/not-found", "Not found", "no such lab")
	case errors.Is(err, labsession.ErrAttemptNotFound):
		return problem(http.StatusNotFound, "/problems/not-found", "Not found", "no such attempt")
	case errors.Is(err, labsession.ErrNoSuchStep):
		return problem(http.StatusNotFound, "/problems/not-found", "Not found", "no such step in this lab")
	case errors.Is(err, labsession.ErrAnotherLabRunning):
		return problem(http.StatusConflict, "/problems/another-lab-running", "Another lab is running",
			"finish or abandon it first")
	case errors.Is(err, labsession.ErrBusy):
		return problem(http.StatusConflict, "/problems/lab-busy", "The lab is busy",
			"another lab operation is running; try again when it ends")
	case errors.As(err, &locked):
		return problem(http.StatusConflict, "/problems/lab-locked", "The lab is locked",
			"pass these labs first: "+strings.Join(locked.Missing, ", "))
	case errors.Is(err, labsession.ErrLabDown):
		return problem(http.StatusConflict, "/problems/lab-down", "The lab is down", "start it with ./opsforge up")
	case errors.Is(err, labsession.ErrUnavailable):
		return problem(http.StatusConflict, "/problems/lab-unavailable", "Not on this layout",
			"this lab needs another lab layout (OPSFORGE_TOPOLOGY)")
	case errors.Is(err, labsession.ErrNotRunning):
		return problem(http.StatusConflict, "/problems/attempt-not-running", "The attempt is not running",
			"it is still setting up, or it has ended")
	case errors.Is(err, labsession.ErrStepOrder):
		return problem(http.StatusConflict, "/problems/step-order", "Check the steps in order",
			"only the first step not passed yet can be checked")
	case errors.Is(err, labsession.ErrNoMoreHints):
		return problem(http.StatusConflict, "/problems/no-more-hints", "No more hints", "every hint of this step is open")
	case errors.Is(err, labsession.ErrResetUnsupported):
		return problem(http.StatusConflict, "/problems/reset-unsupported", "Reset not supported", err.Error())
	case errors.Is(err, labsession.ErrAnswerRequired):
		return problem(http.StatusUnprocessableEntity, "/problems/answer-required", "Answer required",
			"a quiz step is checked with an answer (a choice index)")
	case errors.Is(err, labsession.ErrToolchain):
		return problem(http.StatusServiceUnavailable, "/problems/toolchain", "The lab toolchain failed", err.Error())
	}
	return Problem{}
}

// StartLab starts an attempt and its setup.
func (s *server) StartLab(ctx context.Context, req StartLabRequestObject) (StartLabResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	var course, item string
	if req.Body != nil {
		course, item = deref(req.Body.Course), deref(req.Body.Item)
	}
	a, op, err := s.deps.Labs.Start(ctx, user, req.Lab, course, item)
	if err != nil {
		switch p := labProblem(err); p.Status {
		case http.StatusNotFound:
			return StartLab404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return StartLab409ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusServiceUnavailable:
			return StartLab503ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	v, err := s.deps.Labs.View(ctx, user, a.ID)
	if err != nil {
		return nil, err
	}
	o, err := operation(op)
	if err != nil {
		return nil, err
	}
	return StartLab202JSONResponse{Attempt: labAttempt(v, s.resolver(ctx, v.Namespace)), Operation: o}, nil
}

// GetCurrentAttempt is the learner's running lab, or null.
func (s *server) GetCurrentAttempt(ctx context.Context, _ GetCurrentAttemptRequestObject) (GetCurrentAttemptResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	v, ok, err := s.deps.Labs.Current(ctx, user)
	if err != nil {
		return nil, err
	}
	out := GetCurrentAttempt200JSONResponse{}
	if ok {
		a := labAttempt(v, s.resolver(ctx, v.Namespace))
		out.Attempt = &a
	}
	return out, nil
}

// GetAttempt is one of the learner's attempts.
func (s *server) GetAttempt(ctx context.Context, req GetAttemptRequestObject) (GetAttemptResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.deps.Labs.View(ctx, user, req.Attempt)
	if errors.Is(err, labsession.ErrAttemptNotFound) {
		return GetAttempt404ApplicationProblemPlusJSONResponse(labProblem(err)), nil
	}
	if err != nil {
		return nil, err
	}
	return GetAttempt200JSONResponse(labAttempt(v, s.resolver(ctx, v.Namespace))), nil
}

// CheckStep checks the next step.
func (s *server) CheckStep(ctx context.Context, req CheckStepRequestObject) (CheckStepResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	op, err := s.deps.Labs.Check(ctx, user, req.Attempt, req.Body.Step, req.Body.Answer)
	if err != nil {
		switch p := labProblem(err); p.Status {
		case http.StatusNotFound:
			return CheckStep404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return CheckStep409ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusUnprocessableEntity:
			return CheckStep422ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	o, err := operation(op)
	return CheckStep202JSONResponse(o), err
}

// ShowHint opens the next hint of a step.
func (s *server) ShowHint(ctx context.Context, req ShowHintRequestObject) (ShowHintResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	h, err := s.deps.Labs.Hint(ctx, user, req.Attempt, req.Body.Step)
	if err != nil {
		switch p := labProblem(err); p.Status {
		case http.StatusNotFound:
			return ShowHint404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return ShowHint409ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	return ShowHint200JSONResponse{Step: h.Step, Index: h.Index, Hint: s.resolver(ctx, h.Namespace)(h.Text),
		HintsUsed: h.HintsUsed}, nil
}

// ResetLab sets the lab up again.
func (s *server) ResetLab(ctx context.Context, req ResetLabRequestObject) (ResetLabResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	op, err := s.deps.Labs.Reset(ctx, user, req.Attempt)
	if err != nil {
		switch p := labProblem(err); p.Status {
		case http.StatusNotFound:
			return ResetLab404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return ResetLab409ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	o, err := operation(op)
	return ResetLab202JSONResponse(o), err
}

// AbandonLab ends an attempt without a pass.
func (s *server) AbandonLab(ctx context.Context, req AbandonLabRequestObject) (AbandonLabResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	op, err := s.deps.Labs.Abandon(ctx, user, req.Attempt)
	if err != nil {
		switch p := labProblem(err); p.Status {
		case http.StatusNotFound:
			return AbandonLab404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return AbandonLab409ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	o, err := operation(op)
	return AbandonLab202JSONResponse(o), err
}

// GetOperation is one of the learner's operations.
func (s *server) GetOperation(ctx context.Context, req GetOperationRequestObject) (GetOperationResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	op, err := s.deps.Operations.Get(ctx, user, req.Operation)
	if errors.Is(err, operations.ErrNotFound) {
		return GetOperation404ApplicationProblemPlusJSONResponse(attemptNotFound("operation")), nil
	}
	if err != nil {
		return nil, err
	}
	o, err := operation(op)
	return GetOperation200JSONResponse(o), err
}

// operation is an operation as the API shows it; its result is a labsession.Outcome document.
func operation(op operations.Operation) (Operation, error) {
	out := Operation{Id: op.ID, Kind: OperationKind(op.Kind), Subject: op.Subject, Status: OperationStatus(op.Status),
		Error: op.Error, CreatedAt: op.CreatedAt, FinishedAt: op.FinishedAt}
	if op.Result != nil {
		var r LabOutcome
		if err := json.Unmarshal(op.Result, &r); err != nil {
			return out, err
		}
		out.Result = &r
	}
	return out, nil
}

// labAttempt is an attempt for the page; resolve resolves its hints' placeholders.
func labAttempt(v labsession.View, resolve func(string) string) LabAttempt {
	out := LabAttempt{Id: v.ID, Lab: v.Lab, Course: opt(v.Course), Item: opt(v.Item),
		Status: AttemptStatus(v.Status), StartedAt: v.StartedAt, FinishedAt: v.FinishedAt, Namespace: opt(v.Namespace),
		Workspace: v.Workspace, HintsUsed: v.HintsUsed, Error: opt(v.Error), CurrentStep: opt(v.Current),
		Steps: make([]LabStepState, len(v.Steps))}
	if v.Status == labsession.Passed {
		out.Xp, out.Seconds = &v.XP, &v.Seconds
	}
	for i, st := range v.Steps {
		hints := make(Strings, len(st.Hints))
		for j, h := range st.Hints {
			hints[j] = resolve(h)
		}
		out.Steps[i] = LabStepState{Id: st.ID, Passed: st.Passed, Hints: hints}
	}
	return out
}

// opt is s as a nullable string ("" is null).
func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
