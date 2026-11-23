package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// recoverDetachedWorktree attaches the bead branch to a clean registered
// worktree only when it is exactly the branch head published by origin.
func recoverDetachedWorktree(ctx context.Context, repo, path, branch string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}
	list, err := gitOutRaw(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, fmt.Errorf("list worktrees: %w", err)
	}
	registered := false
	wantPath, _ := filepath.EvalSymlinks(path)
	for _, line := range strings.Split(list, "\x00") {
		if strings.HasPrefix(line, "worktree ") {
			listedPath, _ := filepath.EvalSymlinks(strings.TrimPrefix(line, "worktree "))
			if listedPath == wantPath {
				registered = true
				break
			}
		}
	}
	if !registered {
		return false, fmt.Errorf("path exists but is not a registered worktree")
	}
	if status, err := gitOut(ctx, path, "status", "--porcelain"); err != nil {
		return false, fmt.Errorf("inspect worktree: %w", err)
	} else if status != "" {
		return false, fmt.Errorf("worktree is dirty")
	}
	if _, err := gitOut(ctx, path, "symbolic-ref", "-q", "HEAD"); err == nil {
		return false, fmt.Errorf("worktree is on a branch")
	}
	head, err := gitOut(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return false, fmt.Errorf("read detached HEAD: %w", err)
	}
	origin, err := gitOut(ctx, repo, "rev-parse", "--verify", "refs/remotes/origin/"+branch)
	if err != nil {
		return false, fmt.Errorf("origin/%s is unavailable", branch)
	}
	if head != origin {
		return false, fmt.Errorf("detached HEAD %s differs from origin/%s at %s", head, branch, origin)
	}
	if err := git(ctx, repo, "branch", "-f", branch, head); err != nil {
		return false, fmt.Errorf("move branch to detached HEAD: %w", err)
	}
	if err := git(ctx, path, "checkout", branch); err != nil {
		return false, fmt.Errorf("checkout %s: %w", branch, err)
	}
	return true, nil
}

// sameDir reports whether a and b name the same directory, symlinks
// resolved where they can be.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return real(a) == real(b)
}

// workspaceAt reports whether herdr workspace ws is the checkout at path.
func (h *Herdr) workspaceAt(ctx context.Context, ws, path string) bool {
	raw, err := h.run(ctx, "workspace", "get", ws)
	if err != nil {
		return false
	}
	var r struct {
		Workspace struct {
			Worktree *struct {
				CheckoutPath string `json:"checkout_path"`
			} `json:"worktree"`
		} `json:"workspace"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Workspace.Worktree == nil {
		return false
	}
	return filepath.Clean(r.Workspace.Worktree.CheckoutPath) == filepath.Clean(path)
}
