package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
)

// Importer is what the transport needs from contentimport.
type Importer interface {
	Import(ctx context.Context) (contentimport.Report, error)
}

// ImportContent re-imports the packs.
func (s *server) ImportContent(ctx context.Context, _ ImportContentRequestObject) (ImportContentResponseObject, error) {
	report, err := s.deps.Importer.Import(ctx)
	var invalid *contentimport.BundleErrors
	switch {
	case errors.As(err, &invalid):
		list := make([]PackError, len(invalid.Errors))
		for i, e := range invalid.Errors {
			list[i] = PackError{Pack: e.Pack, Message: e.Message}
		}
		detail := "fix the packs (academy compile lists the errors), then import again"
		return ImportContent422ApplicationProblemPlusJSONResponse{Type: "/problems/invalid-packs",
			Title: "The packs have errors", Status: http.StatusUnprocessableEntity, Detail: &detail, Errors: &list}, nil
	case errors.Is(err, contentimport.ErrCompile):
		detail := err.Error()
		return ImportContent503ApplicationProblemPlusJSONResponse{Type: "/problems/toolchain",
			Title: "The lab toolchain failed", Status: http.StatusServiceUnavailable, Detail: &detail}, nil
	case err != nil:
		return nil, err
	}
	return ImportContent200JSONResponse{Changed: report.Changed, Added: counts(report.Added),
		Updated: counts(report.Updated), Retired: counts(report.Retired), Unchanged: counts(report.Unchanged)}, nil
}

func counts(m map[catalog.Kind]int) EntryCounts {
	out := EntryCounts{}
	for k, n := range m {
		if n > 0 {
			out[string(k)] = n
		}
	}
	return out
}

// Catalog is what the transport needs from catalog.
type Catalog interface {
	Courses(ctx context.Context) ([]catalog.CourseSummary, error)
	Course(ctx context.Context, id string) (catalog.Course, error)
	Item(ctx context.Context, course, id string) (catalog.Item, error)
}

func notFound(what string) NotFoundApplicationProblemPlusJSONResponse {
	detail := what + " is not in the catalogue (or was removed from the packs)"
	return NotFoundApplicationProblemPlusJSONResponse{Type: "/problems/not-found", Title: "Not found",
		Status: http.StatusNotFound, Detail: &detail}
}

// ListCourses is the catalogue.
func (s *server) ListCourses(ctx context.Context, _ ListCoursesRequestObject) (ListCoursesResponseObject, error) {
	courses, err := s.deps.Catalog.Courses(ctx)
	if err != nil {
		return nil, err
	}
	out := ListCourses200JSONResponse{Courses: make([]CourseSummary, len(courses))}
	for i, c := range courses {
		out.Courses[i] = courseSummary(c)
	}
	return out, nil
}

// GetCourse is a course page.
func (s *server) GetCourse(ctx context.Context, req GetCourseRequestObject) (GetCourseResponseObject, error) {
	c, err := s.deps.Catalog.Course(ctx, req.Course)
	if errors.Is(err, catalog.ErrNotFound) {
		return GetCourse404ApplicationProblemPlusJSONResponse{notFound("course " + req.Course)}, nil
	}
	if err != nil {
		return nil, err
	}
	sum := courseSummary(c.CourseSummary)
	out := GetCourse200JSONResponse{Id: sum.Id, Pack: sum.Pack, Title: sum.Title, Subtitle: sum.Subtitle,
		Level: CourseLevel(sum.Level), Minutes: sum.Minutes, Needs: sum.Needs, Requires: sum.Requires,
		Layout: sum.Layout, Badge: sum.Badge, Items: sum.Items, SectionCount: sum.SectionCount,
		ItemCounts: sum.ItemCounts, Description: c.Description,
		Outcomes: strs(c.Outcomes), Requirements: strs(c.Requirements), ContentHash: c.Hash,
		Sections: make([]Section, len(c.Sections))}
	for i, sec := range c.Sections {
		items := make([]ItemRef, len(sec.Items))
		for j, it := range sec.Items {
			items[j] = ItemRef{Id: it.ID, Type: ItemType(it.Type), Title: it.Title, Minutes: it.Minutes,
				ContentHash: it.Hash}
		}
		out.Sections[i] = Section{Title: sec.Title, Layout: layout(sec.Layout), Needs: strs(sec.Needs),
			Minutes: sec.Minutes, Items: items}
	}
	return out, nil
}

// GetItem is one item with its content; nothing earned is in it (catalog's views have no field for it).
func (s *server) GetItem(ctx context.Context, req GetItemRequestObject) (GetItemResponseObject, error) {
	it, err := s.deps.Catalog.Item(ctx, req.Course, req.Item)
	if errors.Is(err, catalog.ErrNotFound) {
		return GetItem404ApplicationProblemPlusJSONResponse{notFound("item " + req.Course + "/" + req.Item)}, nil
	}
	if err != nil {
		return nil, err
	}
	out := GetItem200JSONResponse{Course: it.Course, Id: it.ID, Type: ItemType(it.Type), Title: it.Title,
		Minutes: it.Minutes, Section: it.Section, ContentHash: it.Hash}
	namespace := ""
	if it.Lab != nil && s.deps.Labs != nil { // the learner's running attempt at this lab fills in its "${ns}"
		if user, err := userID(ctx); err == nil {
			if cur, ok, err := s.deps.Labs.Current(ctx, user); err == nil && ok && cur.Lab == it.Lab.ID {
				namespace = cur.Namespace
			}
		}
	}
	resolve := s.resolver(ctx, namespace)
	switch {
	case it.Lecture != nil:
		l := &Lecture{Markdown: resolve(it.Lecture.Markdown), Resources: make([]Resource, len(it.Lecture.Resources))}
		for i, r := range it.Lecture.Resources {
			l.Resources[i] = Resource{Title: r.Title, Url: r.URL}
		}
		out.Lecture = l
	case it.Quiz != nil:
		out.Quiz = &QuizInfo{Pass: it.Quiz.Pass, Xp: it.Quiz.XP, TimeLimitS: it.Quiz.TimeLimitS,
			Questions: it.Quiz.Questions}
	case it.Challenge != nil:
		c := it.Challenge
		starter := c.Starter
		if starter == nil {
			starter = map[string]string{}
		}
		out.Challenge = &ChallengeView{Difficulty: ChallengeViewDifficulty(c.Difficulty), Xp: c.XP,
			Needs: strs(c.Needs), Statement: resolve(c.Statement), Files: strs(c.Files), Starter: starter,
			TimeLimitS: c.TimeLimitS, Tests: strs(c.Tests), Hints: c.Hints}
	case it.Lab != nil:
		out.Lab = labView(it.Lab, resolve)
	}
	return out, nil
}

// labView is a lab for the page; resolve resolves the placeholders of its texts.
func labView(l *catalog.Lab, resolve func(string) string) *LabView {
	v := &LabView{Id: l.ID, Summary: resolve(l.Summary), Xp: l.XP, Needs: strs(l.Needs), Requires: strs(l.Requires),
		Layout: layout(l.Layout), Briefing: resolve(l.Briefing), Debrief: resolve(l.Debrief), Concepts: strs(l.Concepts),
		ClusterChanges: strs(l.ClusterChanges), Namespace: l.Namespace, Files: strs(l.Files),
		Steps: make([]LabStep, len(l.Steps))}
	for i, st := range l.Steps {
		check := StepCheck{Type: StepCheckType(st.Check.Type)}
		switch st.Check.Type {
		case "confirm":
			label := resolve(st.Check.Label)
			check.Label = &label
		case "quiz":
			question, choices := resolve(st.Check.Question), make(Strings, len(st.Check.Choices))
			for j, c := range st.Check.Choices {
				choices[j] = resolve(c)
			}
			check.Question, check.Choices = &question, &choices
		}
		step := LabStep{Id: st.ID, Title: resolve(st.Title), Body: resolve(st.Body), Check: check, Hints: st.Hints}
		if st.Open != "" {
			step.Open = &st.Open
		}
		v.Steps[i] = step
	}
	return v
}

func courseSummary(c catalog.CourseSummary) CourseSummary {
	return CourseSummary{Id: c.ID, Pack: c.Pack, Title: c.Title, Subtitle: c.Subtitle,
		Level: CourseSummaryLevel(c.Level), Minutes: c.Minutes, Needs: strs(c.Needs), Requires: strs(c.Requires),
		Layout: layout(c.Layout), Badge: c.Badge, Items: c.Items, SectionCount: c.Sections,
		ItemCounts: ItemCounts{Lecture: c.Counts.Lecture, Quiz: c.Counts.Quiz, Lab: c.Counts.Lab,
			Challenge: c.Counts.Challenge}}
}

// strs is a list that is never null in JSON.
func strs(s []string) Strings {
	if s == nil {
		return Strings{}
	}
	return s
}

func layout(l *string) *Layout {
	if l == nil {
		return nil
	}
	v := Layout(*l)
	return &v
}
