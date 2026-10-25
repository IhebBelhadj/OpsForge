package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

// Workspaces is what the transport needs from the workspace module.
type Workspaces interface {
	Workdir(ctx context.Context, user int64, ws string) (string, error)
	Tree(ctx context.Context, user int64, ws string) (workspace.Tree, error)
	Read(ctx context.Context, user int64, ws, path string) (workspace.File, error)
	Write(ctx context.Context, user int64, ws, path, content string, base *string) (workspace.File, error)
	Delete(ctx context.Context, user int64, ws, path string) error
	Move(ctx context.Context, user int64, ws, from, to string) error
}

// workspaceProblem maps a workspace error to its problem document (status 0: not a domain error).
func workspaceProblem(err error) Problem {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		return problem(http.StatusNotFound, "/problems/not-found", "Not found", "no such workspace or file")
	case errors.Is(err, workspace.ErrBadPath):
		return problem(http.StatusUnprocessableEntity, "/problems/bad-path", "Bad path",
			"a relative path inside the workspace, without .. or empty segments")
	case errors.Is(err, workspace.ErrConflict):
		return problem(http.StatusConflict, "/problems/conflict", "The file changed",
			"the file changed since it was read; read it again (or save without base_sha256 to overwrite)")
	case errors.Is(err, workspace.ErrExists):
		return problem(http.StatusConflict, "/problems/exists", "Already exists", "the target path exists")
	case errors.Is(err, workspace.ErrTooLarge):
		return problem(http.StatusRequestEntityTooLarge, "/problems/too-large", "Too large",
			"a file holds at most 1 MiB and a workspace 10 MiB")
	case errors.Is(err, workspace.ErrNotText):
		return problem(http.StatusUnsupportedMediaType, "/problems/not-text", "Not a text file",
			"open it in the terminal")
	}
	return Problem{}
}

// GetWorkspaceTree lists a workspace.
func (s *server) GetWorkspaceTree(ctx context.Context, req GetWorkspaceTreeRequestObject) (GetWorkspaceTreeResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.deps.Workspaces.Tree(ctx, user, req.Workspace)
	if p := workspaceProblem(err); p.Status == http.StatusNotFound {
		return GetWorkspaceTree404ApplicationProblemPlusJSONResponse(p), nil
	}
	if err != nil {
		return nil, err
	}
	out := GetWorkspaceTree200JSONResponse{Workspace: req.Workspace, Bytes: t.Bytes, LimitBytes: workspace.MaxTotal,
		Files: make([]WorkspaceEntry, len(t.Files))}
	for i, f := range t.Files {
		out.Files[i] = WorkspaceEntry{Path: f.Path, Size: f.Size}
	}
	return out, nil
}

// ReadWorkspaceFile reads a file.
func (s *server) ReadWorkspaceFile(ctx context.Context, req ReadWorkspaceFileRequestObject) (ReadWorkspaceFileResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.deps.Workspaces.Read(ctx, user, req.Workspace, req.Params.Path)
	if err != nil {
		switch p := workspaceProblem(err); p.Status {
		case http.StatusNotFound:
			return ReadWorkspaceFile404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusUnsupportedMediaType:
			return ReadWorkspaceFile415ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusUnprocessableEntity:
			return ReadWorkspaceFile422ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	return ReadWorkspaceFile200JSONResponse{Path: f.Path, Content: f.Content, Sha256: f.Hash}, nil
}

// WriteWorkspaceFile saves a file.
func (s *server) WriteWorkspaceFile(ctx context.Context, req WriteWorkspaceFileRequestObject) (WriteWorkspaceFileResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.deps.Workspaces.Write(ctx, user, req.Workspace, req.Params.Path, req.Body.Content, req.Body.BaseSha256)
	if err != nil {
		switch p := workspaceProblem(err); p.Status {
		case http.StatusNotFound:
			return WriteWorkspaceFile404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return WriteWorkspaceFile409ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusRequestEntityTooLarge:
			return WriteWorkspaceFile413ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusUnprocessableEntity:
			return WriteWorkspaceFile422ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	return WriteWorkspaceFile200JSONResponse{Path: f.Path, Content: f.Content, Sha256: f.Hash}, nil
}

// DeleteWorkspaceFile deletes a file.
func (s *server) DeleteWorkspaceFile(ctx context.Context, req DeleteWorkspaceFileRequestObject) (DeleteWorkspaceFileResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Workspaces.Delete(ctx, user, req.Workspace, req.Params.Path); err != nil {
		switch p := workspaceProblem(err); p.Status {
		case http.StatusNotFound:
			return DeleteWorkspaceFile404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusUnprocessableEntity:
			return DeleteWorkspaceFile422ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	return DeleteWorkspaceFile204Response{}, nil
}

// MoveWorkspaceFile renames a file.
func (s *server) MoveWorkspaceFile(ctx context.Context, req MoveWorkspaceFileRequestObject) (MoveWorkspaceFileResponseObject, error) {
	user, err := userID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.deps.Workspaces.Move(ctx, user, req.Workspace, req.Body.From, req.Body.To); err != nil {
		switch p := workspaceProblem(err); p.Status {
		case http.StatusNotFound:
			return MoveWorkspaceFile404ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusConflict:
			return MoveWorkspaceFile409ApplicationProblemPlusJSONResponse(p), nil
		case http.StatusUnprocessableEntity:
			return MoveWorkspaceFile422ApplicationProblemPlusJSONResponse(p), nil
		}
		return nil, err
	}
	return MoveWorkspaceFile204Response{}, nil
}
