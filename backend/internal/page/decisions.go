package page

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// MaxDecisions caps one decision log; the most recently published pages'
// decisions are kept.
const MaxDecisions = 500

// ErrBadDecisionState refuses a filter that is neither decided nor undecided.
var ErrBadDecisionState = errors.New("choose decided or undecided, or leave the state out for both")

// Decision is one decision item as the log quotes it, with the page it is on.
type Decision struct {
	PageID        uuid.UUID `json:"pageId"`
	PageTitle     string    `json:"pageTitle"`
	Text          string    `json:"text"`
	State         string    `json:"state"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedByName string    `json:"updatedByName"`
}

// DecisionLog is a space's decisions, newest page first and in page order
// within a page; Truncated says MaxDecisions cut it short.
type DecisionLog struct {
	Decisions []Decision `json:"decisions"`
	Truncated bool       `json:"truncated"`
}

// Decisions is every decision item on the published pages of a space the
// actor may read, out of the trash and the archive; state keeps one state.
func (s *Service) Decisions(ctx context.Context, actor perm.Actor, spaceKey, state string) (*DecisionLog, error) {
	if state != "" && state != document.DecisionDecided && state != document.DecisionUndecided {
		return nil, ErrBadDecisionState
	}
	out := &DecisionLog{Decisions: []Decision{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		// The body is the published one: a draft is its author's until it is.
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, p.body, p.updated_at, COALESCE(u.name, '')
			FROM page p LEFT JOIN app_user u ON u.id = p.updated_by
			WHERE p.space_id = $1 AND`+live+` AND p.archived_at IS NULL AND p.version > 0
			  AND p.body @? '$.** ? (@.type == "decision")' AND `+perm.ViewablePage("p", 2)+`
			ORDER BY p.updated_at DESC, p.id`, sp.ID, actor.UserID)
		if err != nil {
			return fmt.Errorf("read the decisions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				d    Decision
				body []byte
			)
			if err := rows.Scan(&d.PageID, &d.PageTitle, &body, &d.UpdatedAt, &d.UpdatedByName); err != nil {
				return err
			}
			root, err := document.Parse(body)
			if err != nil {
				return fmt.Errorf("read the body of page %s: %w", d.PageID, err)
			}
			for _, n := range decisionsIn(root.Content, 0) {
				d.State, _ = n.Attrs["state"].(string)
				if state != "" && d.State != state {
					continue
				}
				if len(out.Decisions) == MaxDecisions {
					out.Truncated = true
					return nil
				}
				d.Text = document.InlineText(n)
				out.Decisions = append(out.Decisions, d)
			}
		}
		return rows.Err()
	})
	return out, err
}

// decisionsIn is every decision item in blocks, in reading order.
func decisionsIn(blocks []document.Node, depth int) []document.Node {
	if depth > document.MaxDepth {
		return nil
	}
	var out []document.Node
	for _, n := range blocks {
		if n.Type == document.NodeDecision {
			out = append(out, n)
			continue
		}
		out = append(out, decisionsIn(n.Content, depth+1)...)
	}
	return out
}
