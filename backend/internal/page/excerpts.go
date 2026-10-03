package page

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Excerpts lists the excerpts of a page's published body, for a picker: a
// draft's are its author's until it is published, and a folder has none.
func (s *Service) Excerpts(ctx context.Context, actor perm.Actor, id uuid.UUID) ([]document.Excerpt, error) {
	p, _, err := s.Get(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if p.Version == 0 || len(p.Body) == 0 || p.Kind == KindFolder {
		return []document.Excerpt{}, nil
	}
	root, err := document.Parse(p.Body)
	if err != nil {
		return nil, fmt.Errorf("read the body of page %s: %w", id, err)
	}
	return document.Excerpts(root), nil
}
