package checkpoint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/go-git/go-git/v6/plumbing"
)

var archivedGenerationRefPattern = regexp.MustCompile(`^\d{13}$`)

// ListArchivedGenerations returns archived v2 /full/* generation ref suffixes.
// New writes no longer rotate /full/current, but old archives remain readable.
func (s *V2GitStore) ListArchivedGenerations() ([]string, error) {
	refs, err := s.repo.References()
	if err != nil {
		return nil, fmt.Errorf("failed to list references: %w", err)
	}

	var archived []string
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if !strings.HasPrefix(name, paths.V2FullRefPrefix) {
			return nil
		}
		suffix := strings.TrimPrefix(name, paths.V2FullRefPrefix)
		if suffix == "current" || !archivedGenerationRefPattern.MatchString(suffix) {
			return nil
		}
		archived = append(archived, suffix)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate references: %w", err)
	}

	sort.Strings(archived)
	return archived, nil
}
