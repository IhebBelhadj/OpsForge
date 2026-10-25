package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/progress"
)

// Progress is what the transport needs from the progress module.
type Progress interface {
	Overview(ctx context.Context, user int64) ([]progress.CourseProgress, error)
	Course(ctx context.Context, user int64, course string) (progress.CourseProgress, error)
	CompleteLecture(ctx context.Context, user int64, course, item string) (progress.ItemProgress, error)
	Open(ctx context.Context, user int64, course, item string) error
	Note(ctx context.Context, user int64, course, item string) (progress.Note, error)
	SaveNote(ctx context.Context, user int64, course, item, body string) (progress.Note, error)
}

// userID is the signed-in user of a request (guard runs before every handler that calls it).
func userID(ctx context.Context) (int64, error) {
	p, ok := principal(ctx)
	if !ok {
		return 0, errors.New("no principal: guard is not in front of the API")
	}
	return p.User.ID, nil
}

func itemNotFound(course, item string) NotFoundApplicationProblemPlusJSONResponse {
	return notFound("item " + course + "/" + item)
}

// GetProgressOverview is the learner's progress in every course.
func (s *server) GetProgressOverview(ctx context.Context, _ GetProgressOverviewRequestObject) (GetProgressOverviewResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.deps.Progress.Overview(ctx, user)
	if err != nil {
		return nil, err
	}
	out := GetProgressOverview200JSONResponse{Courses: make([]CourseProgress, len(list))}
	for i, p := range list {
		out.Courses[i] = courseProgress(p)
	}
	return out, nil
}

// GetCourseProgress is the learner's progress in a course.
func (s *server) GetCourseProgress(ctx context.Context, req GetCourseProgressRequestObject) (GetCourseProgressResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.deps.Progress.Course(ctx, user, req.Course)
	if errors.Is(err, catalog.ErrNotFound) {
		return GetCourseProgress404ApplicationProblemPlusJSONResponse{notFound("course " + req.Course)}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetCourseProgress200JSONResponse(courseProgress(p)), nil
}

// CompleteLecture marks a lecture completed.
func (s *server) CompleteLecture(ctx context.Context, req CompleteLectureRequestObject) (CompleteLectureResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	ip, err := s.deps.Progress.CompleteLecture(ctx, user, req.Course, req.Item)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return CompleteLecture404ApplicationProblemPlusJSONResponse{itemNotFound(req.Course, req.Item)}, nil
	case errors.Is(err, progress.ErrNotALecture):
		detail := "quizzes, labs and challenges are completed by passing them"
		return CompleteLecture409ApplicationProblemPlusJSONResponse{Type: "/problems/not-a-lecture",
			Title: "Not a lecture", Status: http.StatusConflict, Detail: &detail}, nil
	case err != nil:
		return nil, err
	}
	return CompleteLecture200JSONResponse(itemProgress(ip)), nil
}

// VisitItem records that the learner opened an item.
func (s *server) VisitItem(ctx context.Context, req VisitItemRequestObject) (VisitItemResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	err = s.deps.Progress.Open(ctx, user, req.Course, req.Item)
	if errors.Is(err, catalog.ErrNotFound) {
		return VisitItem404ApplicationProblemPlusJSONResponse{itemNotFound(req.Course, req.Item)}, nil
	}
	if err != nil {
		return nil, err
	}
	return VisitItem204Response{}, nil
}

// GetNote is the learner's note on an item.
func (s *server) GetNote(ctx context.Context, req GetNoteRequestObject) (GetNoteResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.deps.Progress.Note(ctx, user, req.Course, req.Item)
	if errors.Is(err, catalog.ErrNotFound) {
		return GetNote404ApplicationProblemPlusJSONResponse{itemNotFound(req.Course, req.Item)}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetNote200JSONResponse{Body: n.Body, UpdatedAt: n.UpdatedAt}, nil
}

// SaveNote replaces the learner's note on an item.
func (s *server) SaveNote(ctx context.Context, req SaveNoteRequestObject) (SaveNoteResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	n, err := s.deps.Progress.SaveNote(ctx, user, req.Course, req.Item, req.Body.Body)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return SaveNote404ApplicationProblemPlusJSONResponse{itemNotFound(req.Course, req.Item)}, nil
	case errors.Is(err, progress.ErrNoteTooLong):
		detail := "a note holds at most 64 KiB"
		return SaveNote422ApplicationProblemPlusJSONResponse{Type: "/problems/note-too-long",
			Title: "Note too long", Status: http.StatusUnprocessableEntity, Detail: &detail}, nil
	case err != nil:
		return nil, err
	}
	return SaveNote200JSONResponse{Body: n.Body, UpdatedAt: n.UpdatedAt}, nil
}

func courseProgress(p progress.CourseProgress) CourseProgress {
	out := CourseProgress{Course: p.Course, Completed: p.Completed, Total: p.Total, Complete: p.Complete,
		CompletedAt: p.CompletedAt, LastOpenedAt: p.LastOpened, Items: make([]ItemProgress, len(p.Items))}
	if p.Resume != "" {
		out.Resume = &p.Resume
	}
	for i, it := range p.Items {
		out.Items[i] = itemProgress(it)
	}
	return out
}

func itemProgress(ip progress.ItemProgress) ItemProgress {
	return ItemProgress{Id: ip.ID, Type: ItemType(ip.Type), Status: ItemStatus(ip.Status), Stale: ip.Stale,
		CompletedAt: ip.CompletedAt}
}
