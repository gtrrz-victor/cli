package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/logging"
)

// CommittedReader provides read access to committed checkpoint data.
// Both GitStore (v1) and V2GitStore (v2) implement this interface.
type CommittedReader interface {
	ReadCommitted(ctx context.Context, checkpointID id.CheckpointID) (*CheckpointSummary, error)
	ReadSessionContent(ctx context.Context, checkpointID id.CheckpointID, sessionIndex int) (*SessionContent, error)
}

type v2ReaderWithV1RawFallback struct {
	primary  *V2GitStore
	fallback *GitStore
}

func (r *v2ReaderWithV1RawFallback) ReadCommitted(ctx context.Context, checkpointID id.CheckpointID) (*CheckpointSummary, error) {
	return r.primary.ReadCommitted(ctx, checkpointID)
}

func (r *v2ReaderWithV1RawFallback) ReadSessionContent(ctx context.Context, checkpointID id.CheckpointID, sessionIndex int) (*SessionContent, error) {
	content, err := r.primary.ReadSessionContent(ctx, checkpointID, sessionIndex)
	if err == nil {
		return content, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr //nolint:wrapcheck // Propagating context cancellation
	}
	if r.fallback == nil || !errors.Is(err, ErrNoTranscript) {
		return nil, err
	}

	fallbackContent, fallbackErr := r.readFallbackSessionContent(ctx, checkpointID, sessionIndex)
	if fallbackErr == nil {
		return fallbackContent, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr //nolint:wrapcheck // Propagating context cancellation
	}
	return nil, err
}

func (r *v2ReaderWithV1RawFallback) ReadSessionMetadata(ctx context.Context, checkpointID id.CheckpointID, sessionIndex int) (*CommittedMetadata, error) {
	return r.primary.ReadSessionMetadata(ctx, checkpointID, sessionIndex)
}

func (r *v2ReaderWithV1RawFallback) ReadSessionMetadataAndPrompts(ctx context.Context, checkpointID id.CheckpointID, sessionIndex int) (*SessionContent, error) {
	return r.primary.ReadSessionMetadataAndPrompts(ctx, checkpointID, sessionIndex)
}

func (r *v2ReaderWithV1RawFallback) ReadSessionCompactTranscript(ctx context.Context, checkpointID id.CheckpointID, sessionIndex int) ([]byte, error) {
	return r.primary.ReadSessionCompactTranscript(ctx, checkpointID, sessionIndex)
}

func (r *v2ReaderWithV1RawFallback) readFallbackSessionContent(ctx context.Context, checkpointID id.CheckpointID, sessionIndex int) (*SessionContent, error) {
	metadata, err := r.primary.ReadSessionMetadata(ctx, checkpointID, sessionIndex)
	if err == nil && metadata != nil && metadata.SessionID != "" {
		return r.fallback.ReadSessionContentByID(ctx, checkpointID, metadata.SessionID)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr //nolint:wrapcheck // Propagating context cancellation
	}
	if err != nil {
		return nil, err
	}
	return nil, ErrNoTranscript
}

// ReadLatestSessionContent reads the latest session from an already-resolved
// committed reader and summary.
func ReadLatestSessionContent(ctx context.Context, reader CommittedReader, checkpointID id.CheckpointID, summary *CheckpointSummary) (*SessionContent, error) {
	if summary == nil || len(summary.Sessions) == 0 {
		return nil, ErrCheckpointNotFound
	}
	latestIndex := len(summary.Sessions) - 1
	content, err := reader.ReadSessionContent(ctx, checkpointID, latestIndex)
	if err != nil {
		return nil, fmt.Errorf("read session %d content: %w", latestIndex, err)
	}
	return content, nil
}

// ResolveCommittedReaderForCheckpoint resolves which committed checkpoint reader
// should be used for a specific checkpoint ID.
//
// Fallback behavior:
//   - Try v2 /main first when preferCheckpointsV2 is true
//   - Fall back to v1 if v2 /main is missing or unreadable
//   - When v2 /main is chosen, raw transcript reads fall back to the matching
//     v1 session ID if v2 /full/* has no raw transcript
func ResolveCommittedReaderForCheckpoint(
	ctx context.Context,
	checkpointID id.CheckpointID,
	v1Store *GitStore,
	v2Store *V2GitStore,
	preferCheckpointsV2 bool,
) (CommittedReader, *CheckpointSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // Propagating context cancellation
	}

	if preferCheckpointsV2 && v2Store != nil {
		summary, err := v2Store.ReadCommitted(ctx, checkpointID)
		if err == nil && summary != nil {
			if v1Store != nil {
				return &v2ReaderWithV1RawFallback{primary: v2Store, fallback: v1Store}, summary, nil
			}
			return v2Store, summary, nil
		}
		if err != nil && ctx.Err() != nil {
			return nil, nil, ctx.Err() //nolint:wrapcheck // Propagating context cancellation
		}
		if err != nil && !errors.Is(err, ErrCheckpointNotFound) && !errors.Is(err, ErrNoTranscript) {
			logging.Debug(ctx, "v2 ReadCommitted failed, falling back to v1",
				slog.String("checkpoint_id", checkpointID.String()),
				slog.String("error", err.Error()),
			)
		}
	}

	if v1Store == nil {
		return nil, nil, ErrCheckpointNotFound
	}

	summary, err := v1Store.ReadCommitted(ctx, checkpointID)
	if err != nil {
		return nil, nil, err
	}
	if summary == nil {
		return nil, nil, ErrCheckpointNotFound
	}

	return v1Store, summary, nil
}

// ResolveRawSessionLogForCheckpoint resolves the raw transcript log bytes for a
// checkpoint with v2-first, v1-fallback behavior.
//
// Fallback behavior:
//   - Uses ResolveCommittedReaderForCheckpoint
//   - When v2 /main is chosen, raw transcript reads fall back to the matching
//     v1 session ID if v2 /full/* has no raw transcript
func ResolveRawSessionLogForCheckpoint(
	ctx context.Context,
	checkpointID id.CheckpointID,
	v1Store *GitStore,
	v2Store *V2GitStore,
	preferCheckpointsV2 bool,
) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err //nolint:wrapcheck // Propagating context cancellation
	}

	reader, summary, err := ResolveCommittedReaderForCheckpoint(ctx, checkpointID, v1Store, v2Store, preferCheckpointsV2)
	if err != nil {
		return nil, "", err
	}

	content, err := ReadLatestSessionContent(ctx, reader, checkpointID, summary)
	if err != nil {
		return nil, "", err
	}
	return content.Transcript, content.Metadata.SessionID, nil
}
