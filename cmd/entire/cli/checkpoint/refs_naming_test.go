package checkpoint

import (
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
)

func TestRefName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cid  id.CheckpointID
		want plumbing.ReferenceName
	}{
		{
			name: "legacy hex shards on first two",
			cid:  "a1b2c3d4e5f6",
			want: "refs/entire/checkpoints/a1/a1b2c3d4e5f6",
		},
		{
			name: "ulid shards on last two",
			cid:  "01KVBJCWYA4YW6J5M9GP655HZN",
			want: "refs/entire/checkpoints/ZN/01KVBJCWYA4YW6J5M9GP655HZN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, RefName(tt.cid))
		})
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ref    plumbing.ReferenceName
		wantID id.CheckpointID
		wantOK bool
	}{
		{
			name:   "legacy round-trip",
			ref:    "refs/entire/checkpoints/a1/a1b2c3d4e5f6",
			wantID: "a1b2c3d4e5f6",
			wantOK: true,
		},
		{
			name:   "ulid round-trip",
			ref:    "refs/entire/checkpoints/ZN/01KVBJCWYA4YW6J5M9GP655HZN",
			wantID: "01KVBJCWYA4YW6J5M9GP655HZN",
			wantOK: true,
		},
		{
			name:   "wrong prefix",
			ref:    "refs/heads/entire/checkpoints/v1",
			wantOK: false,
		},
		{
			name:   "shard does not match id (legacy in last-two bucket)",
			ref:    "refs/entire/checkpoints/f6/a1b2c3d4e5f6",
			wantOK: false,
		},
		{
			name:   "extra path segment",
			ref:    "refs/entire/checkpoints/a1/a1b2c3d4e5f6/0",
			wantOK: false,
		},
		{
			name:   "missing id",
			ref:    "refs/entire/checkpoints/a1/",
			wantOK: false,
		},
		{
			name:   "missing shard separator",
			ref:    "refs/entire/checkpoints/a1b2c3d4e5f6",
			wantOK: false,
		},
		{
			name:   "prefix only",
			ref:    "refs/entire/checkpoints/",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotID, gotOK := ParseRef(tt.ref)
			assert.Equal(t, tt.wantOK, gotOK)
			if tt.wantOK {
				assert.Equal(t, tt.wantID, gotID)
				// Round-trip: building the ref from the parsed ID reproduces it.
				assert.Equal(t, tt.ref, RefName(gotID))
			} else {
				assert.Equal(t, id.EmptyCheckpointID, gotID)
			}
		})
	}
}
