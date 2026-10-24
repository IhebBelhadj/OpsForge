// Package catalog is the content as imported: packs, courses with their items, lectures, quizzes, challenges, labs
// and badges (docs/courses/content-model.md). Pack files are the source of truth; the catalogue mirrors them
// (contentimport) and never deletes an entry: removed content is retired, as attempts and awards refer to it.
package catalog

import "encoding/json"

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
