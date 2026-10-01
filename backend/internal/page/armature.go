package page

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// syncLinksOf asks the worker to bring a published page's remote links in
// Armature in line with it, when the page names an issue or carries links.
func syncLinksOf(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) error {
	var due bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM armature_endpoint())
		   AND (armature_names_issue(p.body) OR EXISTS (SELECT 1 FROM armature_remote_link l WHERE l.page_id = p.id))
		FROM page p WHERE p.id = $1`, pageID).Scan(&due); err != nil {
		return fmt.Errorf("check the page's Armature links: %w", err)
	}
	if !due {
		return nil
	}
	return events.Emit(ctx, tx, events.TopicArmatureLinks, events.ArmatureLinks{PageID: pageID, ActorID: actor.UserID})
}

// syncLinksBelow does the same for the pages named and, with below, every
// page under them, which the actor may not all see; the database writes the
// events, so it never answers which pages those are.
func syncLinksBelow(ctx context.Context, tx db.DBTX, roots []uuid.UUID, below bool) error {
	if _, err := tx.Exec(ctx, `SELECT armature_links_emit($1, $2, $3)`, roots, below, events.TraceParent(ctx)); err != nil {
		return fmt.Errorf("sync the pages' Armature links: %w", err)
	}
	return nil
}

// syncLinksOfSpace does it for every page of a space, or with trashed for
// every page in its trash.
func syncLinksOfSpace(ctx context.Context, tx db.DBTX, spaceID uuid.UUID, trashed bool) error {
	if _, err := tx.Exec(ctx, `SELECT armature_links_emit_space($1, $2, $3)`, spaceID, trashed, events.TraceParent(ctx)); err != nil {
		return fmt.Errorf("sync the space's Armature links: %w", err)
	}
	return nil
}
