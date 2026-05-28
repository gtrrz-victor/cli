//nolint:ireturn // billy.Filesystem/File passthrough requires interface returns.
package gitrepo

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
)

// alternatesFilePath is the repository-relative path of the alternates file,
// in slash form, as go-git opens it.
const alternatesFilePath = "objects/info/alternates"

// maxAlternatesReadBytes caps how much of the alternates file we read. Real
// alternates files hold a handful of short paths; the limit is purely
// defensive against an oversized or malformed file. Content past the cap is
// dropped, and a trailing line truncated by the cap is discarded rather
// than fed to the rewrite logic as a bogus path.
const maxAlternatesReadBytes = 4096

// alternatesRewriteFS wraps a git-directory filesystem and rewrites the
// objects/info/alternates file on read so that relative alternate object
// directories are presented to go-git as absolute paths.
//
// go-git cannot follow relative alternates: its dotgit.Alternates() strips any
// leading "../" and anchors the remainder at the filesystem root (see the
// upstream comment in storage/filesystem/dotgit/dotgit.go), which mangles a
// relative entry such as "../../entirehq/entiredb/.git/objects" into a
// non-existent absolute path. Git itself resolves relative alternates against
// $GIT_DIR/objects, so we resolve them the same way and hand go-git an absolute
// path, which it follows correctly via the OS-rooted AlternatesFS.
//
// Absolute entries are passed through untouched; if no entry needs rewriting
// the original file is served unchanged.
type alternatesRewriteFS struct {
	billy.Filesystem // wrapped git-dir FS; promotes the full billy interface
}

// wrapAlternatesRewrite wraps fs so reads of objects/info/alternates resolve
// relative entries to absolute paths. fs must be rooted at a git directory
// (its Root() joined with "objects" is the base for relative alternates).
func wrapAlternatesRewrite(fs billy.Filesystem) billy.Filesystem {
	return &alternatesRewriteFS{Filesystem: fs}
}

func (fs *alternatesRewriteFS) Open(filename string) (billy.File, error) {
	if isAlternatesFile(filename) {
		if content, ok := fs.absolutizedAlternates(); ok {
			return inMemoryFile(content)
		}
	}
	return fs.Filesystem.Open(filename) //nolint:wrapcheck // preserve underlying FS errors
}

func isAlternatesFile(filename string) bool {
	return filepath.ToSlash(filepath.Clean(filename)) == alternatesFilePath
}

// absolutizedAlternates reads the wrapped alternates file (capped at
// maxAlternatesReadBytes) and returns a copy of its contents with every
// relative entry resolved against <root>/objects. Absolute entries, blank
// lines, and comment lines (those starting with '#') are preserved
// unchanged. ok is false when the file is missing/unreadable or no entry
// needed rewriting, in which case the caller serves the original file.
func (fs *alternatesRewriteFS) absolutizedAlternates() (string, bool) {
	f, err := fs.Filesystem.Open(filepath.FromSlash(alternatesFilePath))
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()

	lines, ok := readAlternatesLines(f)
	if !ok {
		return "", false
	}
	return rewriteRelativeAlternates(lines, filepath.Join(fs.Root(), "objects"))
}

// readAlternatesLines reads up to maxAlternatesReadBytes from r and splits
// the content on '\n'. When the file exceeds the cap, a trailing line that
// has no closing newline is discarded so we never hand a truncated path to
// the rewrite logic.
func readAlternatesLines(r io.Reader) ([]string, bool) {
	// Read one byte past the cap so we can tell whether the underlying file
	// has more data than fits in the budget.
	data, err := io.ReadAll(io.LimitReader(r, maxAlternatesReadBytes+1))
	if err != nil {
		return nil, false
	}
	truncated := false
	if len(data) > maxAlternatesReadBytes {
		data = data[:maxAlternatesReadBytes]
		truncated = true
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	if truncated && !strings.HasSuffix(text, "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines, true
}

// rewriteRelativeAlternates rewrites every relative entry in lines against
// objectsBase. Blank lines, comments ('#'-prefixed), and already-absolute
// entries are left untouched. Returns (joined-content, true) when at least
// one relative entry was rewritten; otherwise ok=false so the caller serves
// the original file unchanged.
func rewriteRelativeAlternates(lines []string, objectsBase string) (string, bool) {
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if filepath.IsAbs(trimmed) || filepath.VolumeName(trimmed) != "" {
			continue
		}
		lines[i] = filepath.Clean(filepath.Join(objectsBase, filepath.FromSlash(trimmed)))
		changed = true
	}
	if !changed {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// isAlternatesObjectsPath reports whether absPath looks like an alternates
// file located at <objects-dir>/info/alternates. Used by the OS-rooted
// alternates filesystem to detect nested alternates files.
func isAlternatesObjectsPath(absPath string) bool {
	clean := filepath.Clean(absPath)
	return filepath.Base(clean) == "alternates" &&
		filepath.Base(filepath.Dir(clean)) == "info"
}

func inMemoryFile(content string) (billy.File, error) {
	mem := memfs.New()
	f, err := mem.Create(alternatesFilePath)
	if err != nil {
		return nil, err //nolint:wrapcheck // memfs errors are descriptive enough
	}
	if _, err := f.Write([]byte(content)); err != nil {
		_ = f.Close()
		return nil, err //nolint:wrapcheck // memfs errors are descriptive enough
	}
	if err := f.Close(); err != nil {
		return nil, err //nolint:wrapcheck // memfs errors are descriptive enough
	}
	return mem.Open(alternatesFilePath) //nolint:wrapcheck // memfs errors are descriptive enough
}
