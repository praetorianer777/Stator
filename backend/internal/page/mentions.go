package page

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Mentionable are the people the editor's at sign offers on a page, each
// saying whether they may view it once it is published.
func (s *Service) Mentionable(ctx context.Context, actor perm.Actor, id uuid.UUID, q string, limit int) ([]perm.Mentionable, error) {
	var out []perm.Mentionable
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, _, err := load(ctx, tx, actor, id, false); err != nil {
			return err
		}
		var err error
		out, err = perm.MentionablePeople(ctx, tx, id, q, limit)
		return err
	})
	return out, err
}

// newlyMentioned are the people a version names that the version before it
// did not, narrowed to those a mention tells.
func newlyMentioned(ctx context.Context, tx db.DBTX, pageID uuid.UUID, previous int, body json.RawMessage) ([]uuid.UUID, error) {
	var before []document.Mention
	if previous > 0 {
		var old json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT body FROM page_version WHERE page_id = $1 AND number = $2`, pageID, previous).Scan(&old); err != nil {
			return nil, fmt.Errorf("read version %d: %w", previous, err)
		}
		before = document.MentionsIn(old)
	}
	return perm.MentionsToTell(ctx, tx, pageID, document.MentionIDs(document.NewMentions(before, document.MentionsIn(body))))
}
