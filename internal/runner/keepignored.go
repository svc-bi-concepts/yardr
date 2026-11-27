package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// notKept are the trees a worktree holds that a tool regenerates, and yardr's
// own state: no data, so closing a bead lets them go with the worktree. Only
// these are skipped; every other file git ignores is kept (yardr-ztz9: we
// delete no data). A name is matched on every path component, so a nested
// node_modules goes too.
var notKept = map[string]bool{
	"node_modules": true, ".venv": true, "__pycache__": true, ".pytest_cache": true,
	".mypy_cache": true, ".ruff_cache": true, ".next": true, ".turbo": true,
	".parcel-cache": true, ".gradle": true, "target": true, ".yard": true,
}

// keptMoved is what keepIgnored did: how many files it moved to the kept
// directory, and the credential files it left in the tree, by path. A
// credential is not data (the mayor's rule), so it goes with the worktree.
type keptMoved struct {
	Files int
	Left  []string
}

// keepIgnored moves every file under dir that git ignores, and that is not
// in notKept, to kept, keeping its path below dir and its mode. gitDir is
// the git dir whose ignore rules decide what is ignored. A name already in
// kept gets a numbered suffix, so nothing is overwritten. A move that fails
// stops the walk and returns its error: the files already moved stay in
// kept, and the rest stay in dir, so the next close picks up the rest.
func keepIgnored(ctx context.Context, gitDir, dir, kept string) (keptMoved, error) {
	var res keptMoved
	rels, err := ignoredData(ctx, gitDir, dir)
	if err != nil {
		return res, err
	}
	for _, rel := range rels {
		if isCredential(rel) {
			res.Left = append(res.Left, rel)
			continue
		}
		if kept == "" {
			return res, errors.New("no kept directory for the private data of a worktree")
		}
		if err := os.MkdirAll(kept, 0o700); err != nil {
			return res, fmt.Errorf("make kept directory: %w", plainErr(err))
		}
		target, err := freeName(filepath.Join(kept, rel))
		if err != nil {
			return res, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return res, fmt.Errorf("make kept directory: %w", plainErr(err))
		}
		if err := moveFile(filepath.Join(dir, rel), target); err != nil {
			return res, fmt.Errorf("move private data to kept directory after %d file(s): %w", res.Files, plainErr(err))
		}
		res.Files++
	}
	return res, nil
}

// ignoredData lists, relative to dir, the files git ignores under dir that
// are not notKept. An ignored directory git lists collapsed is walked here;
// a notKept name is skipped wherever it is on the path.
func ignoredData(ctx context.Context, gitDir, dir string) ([]string, error) {
	out, err := gitOutRaw(ctx, dir, "--git-dir="+gitDir, "--work-tree="+dir,
		"ls-files", "-z", "-o", "-i", "--exclude-standard", "--directory")
	if err != nil {
		return nil, fmt.Errorf("list ignored files of %s: %w", dir, err)
	}
	var rels []string
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" || hasNotKept(entry) {
			continue
		}
		if !strings.HasSuffix(entry, "/") {
			rels = append(rels, entry)
			continue
		}
		root := filepath.Join(dir, entry)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if notKept[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			rels = append(rels, rel)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk ignored %s: %w", entry, plainErr(err))
		}
	}
	sort.Strings(rels)
	return rels, nil
}

// hasNotKept reports whether a path names a notKept tree at any depth.
func hasNotKept(rel string) bool {
	for _, part := range strings.Split(strings.Trim(rel, "/"), "/") {
		if notKept[part] {
			return true
		}
	}
	return false
}

// isCredential is the rule of integrations/bin/keep-private.sh: a file that
// holds a secret, not data. The names are the basenames it matches.
func isCredential(rel string) bool {
	name := filepath.Base(rel)
	switch {
	case name == ".env", strings.HasPrefix(name, ".env."):
		return true
	case strings.HasSuffix(name, ".key"), strings.HasSuffix(name, ".pem"):
		return true
	}
	return name == "secret-id" || name == "token"
}

// freeName is target, or target with a numbered suffix when a file is
// already there: a name taken is never overwritten.
func freeName(target string) (string, error) {
	name := target
	for i := 1; ; i++ {
		_, err := os.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("check kept name: %w", plainErr(err))
		}
		name = fmt.Sprintf("%s.%d", target, i)
	}
}

// moveFile renames src to dst, which is a rename on one volume. Across
// volumes (a worktree root on another disk) it copies, syncs and only then
// removes src, so there is never a moment the data is in neither place.
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return os.Remove(src)
}

// plainErr is err without the paths a *PathError or *LinkError carries: what
// is logged names no file under private/, which can be personal data.
func plainErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

// keptDir is the directory the data of one bead's worktree is kept in: under
// the yard's home, outside every worktree and every git depot, per depot and
// per bead, so beads never mix. "" when the yard's home or the bead is
// unknown; keepIgnored then refuses to move anything.
func (h *Herdr) keptDir(handle map[string]any, path string) string {
	depot, _ := handle["depot"].(string)
	bead, _ := handle["bead"].(string)
	if bead == "" {
		// A worktree is made at <root>/<depot>/<bead> (worktreePath); a
		// handle from before depot and bead were recorded is read that way.
		bead = filepath.Base(path)
		depot = filepath.Base(filepath.Dir(path))
	}
	if h.YardHome == "" || depot == "" || bead == "" {
		return ""
	}
	return filepath.Join(h.YardHome, "kept", depot, bead)
}

// keepPrivate moves the ignored data of the worktree at dir (its git dir is
// gitDir) to the bead's kept directory, and records what it did as a
// private_kept event: the directory, a file count and the credential paths
// left behind. Nothing is logged when nothing was there.
func (h *Herdr) keepPrivate(ctx context.Context, gitDir, dir string, handle map[string]any) error {
	kept := h.keptDir(handle, dir)
	res, err := keepIgnored(ctx, gitDir, dir, kept)
	if err != nil {
		return err
	}
	bead, _ := handle["bead"].(string)
	if (res.Files == 0 && len(res.Left) == 0) || h.Store == nil || bead == "" {
		return nil
	}
	return h.Store.LogDispatch(ctx, bead, "private_kept", "yard", map[string]any{
		"worktree": dir, "kept": kept, "files": res.Files, "credentials_left": res.Left})
}
