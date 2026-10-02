// Package hub is the organization's hub: one of its pages, chosen by its
// administrators, that everybody finds from the navigation and may land on.
package hub

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// FieldError refuses one field of a choice, in a sentence the form shows under it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// ErrNotAdmin refuses a choice of hub by anybody but an administrator.
var ErrNotAdmin = errors.New("only an administrator of the organization chooses its hub")

// HubPage is the hub page as the reader may see it.
type HubPage struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	SpaceKey string    `json:"spaceKey"`
}

// Hub is the organization's hub for the reader. Page is null when there is
// none, or when the reader may not see it or it is in the trash or archived;
// Landing then sends nobody anywhere.
type Hub struct {
	Page    *HubPage `json:"page"`
	Landing bool     `json:"landing"`
}

// HubInput chooses the hub, or none with a null page, and whether everybody
// lands on it.
type HubInput struct {
	PageID  *uuid.UUID `json:"pageId"`
	Landing bool       `json:"landing"`
}

type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// read is the hub as the actor sees it, in the caller's transaction.
func read(ctx context.Context, tx db.DBTX, actor perm.Actor) (*Hub, error) {
	var (
		out             Hub
		id              *uuid.UUID
		title, spaceKey *string
	)
	err := tx.QueryRow(ctx, `
		SELECT o.hub_landing, p.id, p.title, s.key
		FROM org o
		LEFT JOIN page p ON p.id = o.hub_page_id AND p.trashed_at IS NULL AND p.archived_at IS NULL AND `+perm.ViewablePage("p", 1)+`
		LEFT JOIN space s ON s.id = p.space_id
		WHERE o.id = current_org_id()`, actor.UserID).Scan(&out.Landing, &id, &title, &spaceKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return &out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the hub: %w", err)
	}
	if id != nil && title != nil && spaceKey != nil {
		out.Page = &HubPage{ID: *id, Title: *title, SpaceKey: *spaceKey}
	} else {
		out.Landing = false
	}
	return &out, nil
}

// Get is the hub as the actor sees it.
func (s *Service) Get(ctx context.Context, actor perm.Actor) (*Hub, error) {
	var out *Hub
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = read(ctx, tx, actor)
		return err
	})
	return out, err
}

// Set chooses the hub. The page has to be one the administrator sees, out
// of the trash and the archive; nobody lands on a hub that is not there.
func (s *Service) Set(ctx context.Context, actor perm.Actor, in HubInput) (*Hub, db.LSN, error) {
	if in.PageID == nil && in.Landing {
		return nil, 0, &FieldError{Field: "landing", Message: "Choose a hub page before everybody lands on it."}
	}
	var out *Hub
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := perm.LoadFacts(ctx, tx, actor, uuid.Nil)
		if err != nil {
			return err
		}
		if !f.OrgAdmin() {
			return ErrNotAdmin
		}
		data := map[string]any{"landing": in.Landing}
		if in.PageID != nil {
			var title string
			err := tx.QueryRow(ctx, `SELECT p.title FROM page p WHERE p.id = $1 AND p.trashed_at IS NULL AND p.archived_at IS NULL AND `+perm.ViewablePage("p", 2),
				*in.PageID, actor.UserID).Scan(&title)
			if errors.Is(err, pgx.ErrNoRows) {
				return &FieldError{Field: "pageId", Message: "That page was not found, or is in the trash or archived. Choose another page for the hub."}
			}
			if err != nil {
				return fmt.Errorf("find the hub page: %w", err)
			}
			data["title"] = title
		}
		if _, err := tx.Exec(ctx, `UPDATE org SET hub_page_id = $1, hub_landing = $2 WHERE id = current_org_id()`, in.PageID, in.Landing); err != nil {
			return fmt.Errorf("choose the hub: %w", err)
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionOrgHubSet, "page", in.PageID, data); err != nil {
			return err
		}
		out, err = read(ctx, tx, actor)
		return err
	})
	return out, lsn, err
}
