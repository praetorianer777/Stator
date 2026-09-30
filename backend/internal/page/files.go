package page

import (
	"context"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// Load reads a page for another domain acting on it, answering ErrNotFound
// for what the actor may not see and what is in the trash.
func Load(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*Page, *space.Space, error) {
	return load(ctx, tx, actor, id, false)
}

// CopyObserver is told of a copy inside its transaction, each copied page's
// id mapped to its copy's, so what hangs off a page comes along.
type CopyObserver interface {
	PagesCopied(ctx context.Context, tx db.DBTX, copies map[uuid.UUID]uuid.UUID) error
}

// ObserveCopies has o told of every copy from now on.
func (s *Service) ObserveCopies(o CopyObserver) {
	s.copyObservers = append(s.copyObservers, o)
}

// copied is one page of a copy and the page it was copied from.
type copied struct {
	From, To uuid.UUID
}
