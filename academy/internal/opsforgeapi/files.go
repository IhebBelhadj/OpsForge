package opsforgeapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

// Files is the workspace module's FileStore on OpsForge: the learners' trees live on the lab host, where their
// terminals run (`/v1/workspaces/...`). OpsForge confines paths to the tree and caps sizes too.
type Files struct {
	c *Client
}

var _ workspace.FileStore = Files{}

// Files returns the workspace store.
func (c *Client) Files() Files { return Files{c: c} }

func (f Files) path(ws string, file ...string) string {
	p := "/v1/workspaces/" + url.PathEscape(ws)
	if len(file) > 0 {
		segs := strings.Split(file[0], "/")
		for i, s := range segs {
			segs[i] = url.PathEscape(s)
		}
		p += "/files/" + strings.Join(segs, "/")
	}
	return p
}

// mapErr turns OpsForge's problems into the workspace module's errors.
func mapErr(err error) error {
	var p *Problem
	if !errors.As(err, &p) {
		return err
	}
	switch {
	case p.Status == http.StatusNotFound:
		return fmt.Errorf("%w: %s", workspace.ErrNotFound, p.Detail)
	case p.Status == http.StatusConflict:
		return fmt.Errorf("%w: %s", workspace.ErrExists, p.Detail)
	case p.Status == http.StatusRequestEntityTooLarge:
		return fmt.Errorf("%w: %s", workspace.ErrTooLarge, p.Detail)
	case p.Slug() == "bad-path" || p.Slug() == "bad-workspace":
		return fmt.Errorf("%w: %s", workspace.ErrBadPath, p.Detail)
	}
	return err
}

func (f Files) send(ctx context.Context, method, path string, body []byte, want int) error {
	ctx, cancel := context.WithTimeout(ctx, f.c.Timeout)
	defer cancel()
	var r io.Reader
	contentType := ""
	if body != nil {
		r, contentType = bytes.NewReader(body), "application/octet-stream"
	}
	resp, err := f.c.do(ctx, method, path, r, contentType, want)
	if err != nil {
		return mapErr(err)
	}
	return resp.Body.Close()
}

// Create makes an empty tree; ErrExists if there is one.
func (f Files) Create(ctx context.Context, ws string) error {
	return f.send(ctx, http.MethodPut, f.path(ws), nil, http.StatusCreated)
}

// List is the tree's regular files, by path.
func (f Files) List(ctx context.Context, ws string) ([]workspace.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, f.c.Timeout)
	defer cancel()
	var doc struct {
		Files []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if err := f.c.call(ctx, http.MethodGet, f.path(ws)+"/files", nil, "filesOutput", &doc, http.StatusOK); err != nil {
		return nil, mapErr(err)
	}
	out := make([]workspace.Entry, len(doc.Files))
	for i, e := range doc.Files {
		out[i] = workspace.Entry{Path: e.Path, Size: e.Size}
	}
	return out, nil
}

// Read returns a file's content; ErrNotFound if there is none.
func (f Files) Read(ctx context.Context, ws, p string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, f.c.Timeout)
	defer cancel()
	resp, err := f.c.do(ctx, http.MethodGet, f.path(ws, p), nil, "", http.StatusOK)
	if err != nil {
		return nil, mapErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, workspace.MaxFile+1))
	if err != nil {
		return nil, fmt.Errorf("opsforge workspace %s: %w", ws, err)
	}
	return data, nil
}

// Write replaces (or creates, with its folders) a file.
func (f Files) Write(ctx context.Context, ws, p string, data []byte) error {
	if data == nil {
		data = []byte{}
	}
	return f.send(ctx, http.MethodPut, f.path(ws, p), data, http.StatusNoContent)
}

// Remove deletes a file; ErrNotFound if there is none.
func (f Files) Remove(ctx context.Context, ws, p string) error {
	return f.send(ctx, http.MethodDelete, f.path(ws, p), nil, http.StatusNoContent)
}

// Rename moves a file (creating the target's folders); ErrNotFound if there is none, ErrExists onto a file.
func (f Files) Rename(ctx context.Context, ws, from, to string) error {
	ctx, cancel := context.WithTimeout(ctx, f.c.Timeout)
	defer cancel()
	in := struct {
		Contract int    `json:"contract"`
		From     string `json:"from"`
		To       string `json:"to"`
	}{Contract, from, to}
	resp, err := f.c.do(ctx, http.MethodPost, f.path(ws)+"/moves", jsonBody(in), "application/json",
		http.StatusNoContent)
	if err != nil {
		return mapErr(err)
	}
	return resp.Body.Close()
}
