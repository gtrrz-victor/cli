package gitrepo

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/require"
)

func TestAbsolutizedAlternates(t *testing.T) {
	t.Parallel()

	const relPath = "alt/objects"
	const relPathA = "altA/objects"
	const relPathB = "altB/objects"
	rewritten := filepath.Clean(filepath.Join("/objects", relPath))
	rewrittenA := filepath.Clean(filepath.Join("/objects", relPathA))
	rewrittenB := filepath.Clean(filepath.Join("/objects", relPathB))

	tests := []struct {
		name    string
		content string
		want    string
		wantOk  bool
	}{
		{
			name:    "relative path is absolutized",
			content: relPath + "\n",
			want:    rewritten + "\n",
			wantOk:  true,
		},
		{
			name:    "relative path without trailing newline",
			content: relPath,
			want:    rewritten,
			wantOk:  true,
		},
		{
			name:    "absolute path returns not ok",
			content: "/already/absolute/objects\n",
			wantOk:  false,
		},
		{
			name:    "empty file returns not ok",
			content: "",
			wantOk:  false,
		},
		{
			name:    "comment line is preserved, relative path rewritten",
			content: "#comment\n" + relPath + "\n",
			want:    "#comment\n" + rewritten + "\n",
			wantOk:  true,
		},
		{
			name:    "multiple comments preserved before relative path",
			content: "#one\n#two\n#three\n" + relPath + "\n",
			want:    "#one\n#two\n#three\n" + rewritten + "\n",
			wantOk:  true,
		},
		{
			name:    "leading whitespace before # still treated as comment",
			content: "   #comment\n" + relPath + "\n",
			want:    "   #comment\n" + rewritten + "\n",
			wantOk:  true,
		},
		{
			name:    "blank lines preserved around relative path",
			content: "\n\n" + relPath + "\n",
			want:    "\n\n" + rewritten + "\n",
			wantOk:  true,
		},
		{
			name:    "file containing only comments returns not ok",
			content: "#one\n#two\n#three\n",
			wantOk:  false,
		},
		{
			name:    "two relative alternates are both rewritten",
			content: relPathA + "\n" + relPathB + "\n",
			want:    rewrittenA + "\n" + rewrittenB + "\n",
			wantOk:  true,
		},
		{
			name:    "mix of relative and absolute preserves order",
			content: relPathA + "\n/already/absolute/objects\n" + relPathB + "\n",
			want:    rewrittenA + "\n/already/absolute/objects\n" + rewrittenB + "\n",
			wantOk:  true,
		},
		{
			name:    "absolute first then relative",
			content: "/already/absolute/objects\n" + relPath + "\n",
			want:    "/already/absolute/objects\n" + rewritten + "\n",
			wantOk:  true,
		},
		{
			name:    "comment between alternates is preserved",
			content: relPathA + "\n#note\n" + relPathB + "\n",
			want:    rewrittenA + "\n#note\n" + rewrittenB + "\n",
			wantOk:  true,
		},
		{
			name:    "all absolute alternates return not ok",
			content: "/abs/one\n/abs/two\n/abs/three\n",
			wantOk:  false,
		},
		{
			name: "data beyond maxAlternatesReadBytes is ignored",
			// A 4096-byte comment fills the entire read budget; the relative
			// path that follows sits past the limit and must not be reached.
			content: strings.Repeat("#", maxAlternatesReadBytes) + "\n" + relPath + "\n",
			wantOk:  false,
		},
		{
			name: "trailing partial line past the cap is discarded",
			// The usable path sits at offset 0 and a giant unterminated tail
			// follows; the tail is truncated by the cap and must not be
			// treated as a second relative entry.
			content: relPath + "\n" + strings.Repeat("x", maxAlternatesReadBytes*4),
			want:    rewritten,
			wantOk:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := newAlternatesFS(t, tc.content)
			got, ok := fs.absolutizedAlternates()
			require.Equal(t, tc.wantOk, ok)
			if tc.wantOk {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func newAlternatesFS(t *testing.T, content string) *alternatesRewriteFS {
	t.Helper()
	mfs := memfs.New()
	f, err := mfs.Create(alternatesFilePath)
	require.NoError(t, err)
	_, err = f.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return &alternatesRewriteFS{Filesystem: mfs}
}
