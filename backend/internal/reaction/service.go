package reaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Service puts reactions on and takes them off. Reading them is OnPage and
// OnComments, which the page and comment services call in their own reads.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// target is what a reaction goes on: a page, or a comment of it.
type target struct {
	page    uuid.UUID
	comment *uuid.UUID
}

// viewable loads the page the actor may view, out of the trash, and says
// whether they may react to it.
func viewable(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) (published, may bool, err error) {
	var version int
	err = tx.QueryRow(ctx, `
		SELECT p.version FROM page p
		WHERE p.id = $1 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 2), pageID, actor.UserID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, ErrPageNotFound
	}
	if err != nil {
		return false, false, err
	}
	access, _, err := perm.ForPage(ctx, tx, actor, pageID)
	if err != nil {
		return false, false, err
	}
	if !access.View {
		return false, false, ErrPageNotFound
	}
	return version > 0, access.Comment, nil
}

// onComment finds a comment the actor may see, on a page out of the trash; a
// deleted one is not found when it is to take a reaction.
func onComment(ctx context.Context, tx db.DBTX, actor perm.Actor, commentID uuid.UUID, live bool) (target, bool, bool, error) {
	var (
		pageID  uuid.UUID
		deleted bool
	)
	err := tx.QueryRow(ctx, `SELECT page_id, deleted_at IS NOT NULL FROM comment WHERE id = $1`, commentID).Scan(&pageID, &deleted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && live && deleted) {
		return target{}, false, false, ErrCommentNotFound
	}
	if err != nil {
		return target{}, false, false, err
	}
	published, may, err := viewable(ctx, tx, actor, pageID)
	if errors.Is(err, ErrPageNotFound) {
		return target{}, false, false, ErrCommentNotFound
	}
	return target{page: pageID, comment: &commentID}, published, may, err
}

func (s *Service) add(ctx context.Context, actor perm.Actor, emoji string, locate func(context.Context, db.DBTX) (target, bool, bool, error)) ([]Reaction, db.LSN, error) {
	emoji, err := Clean(emoji)
	if err != nil {
		return nil, 0, err
	}
	var out []Reaction
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		on, published, may, err := locate(ctx, tx)
		if err != nil {
			return err
		}
		if !published {
			return ErrUnpublished
		}
		if !may {
			return ErrMayNotReact
		}
		var held int
		var already bool
		if err := tx.QueryRow(ctx, `
			SELECT count(*), coalesce(bool_or(emoji = $4), false) FROM reaction
			WHERE page_id = $1 AND comment_id IS NOT DISTINCT FROM $2 AND user_id = $3`,
			on.page, on.comment, actor.UserID, emoji).Scan(&held, &already); err != nil {
			return fmt.Errorf("count the reactions: %w", err)
		}
		if !already && held >= MaxPerPerson {
			return &FieldError{Field: "emoji", Message: ErrTooMany.Error(), err: ErrTooMany}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reaction (org_id, page_id, comment_id, user_id, emoji)
			VALUES (current_org_id(), $1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, on.page, on.comment, actor.UserID, emoji); err != nil {
			return fmt.Errorf("add the reaction: %w", err)
		}
		out, err = of(ctx, tx, actor.UserID, on)
		return err
	})
	return out, lsn, err
}

func (s *Service) remove(ctx context.Context, actor perm.Actor, emoji string, locate func(context.Context, db.DBTX) (target, bool, bool, error)) ([]Reaction, db.LSN, error) {
	emoji, err := Clean(emoji)
	if err != nil {
		return nil, 0, err
	}
	var out []Reaction
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		on, _, _, err := locate(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM reaction
			WHERE page_id = $1 AND comment_id IS NOT DISTINCT FROM $2 AND user_id = $3 AND emoji = $4`,
			on.page, on.comment, actor.UserID, emoji); err != nil {
			return fmt.Errorf("take the reaction off: %w", err)
		}
		out, err = of(ctx, tx, actor.UserID, on)
		return err
	})
	return out, lsn, err
}

func pageTarget(actor perm.Actor, pageID uuid.UUID) func(context.Context, db.DBTX) (target, bool, bool, error) {
	return func(ctx context.Context, tx db.DBTX) (target, bool, bool, error) {
		published, may, err := viewable(ctx, tx, actor, pageID)
		return target{page: pageID}, published, may, err
	}
}

func commentTarget(actor perm.Actor, commentID uuid.UUID, live bool) func(context.Context, db.DBTX) (target, bool, bool, error) {
	return func(ctx context.Context, tx db.DBTX) (target, bool, bool, error) {
		return onComment(ctx, tx, actor, commentID, live)
	}
}

// ReactToPage puts the caller's emoji on a page; putting it on twice is no change.
func (s *Service) ReactToPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID, emoji string) ([]Reaction, db.LSN, error) {
	return s.add(ctx, actor, emoji, pageTarget(actor, pageID))
}

// UnreactPage takes the caller's emoji off a page, if it is there.
func (s *Service) UnreactPage(ctx context.Context, actor perm.Actor, pageID uuid.UUID, emoji string) ([]Reaction, db.LSN, error) {
	return s.remove(ctx, actor, emoji, pageTarget(actor, pageID))
}

// ReactToComment puts the caller's emoji on a comment that is not deleted.
func (s *Service) ReactToComment(ctx context.Context, actor perm.Actor, commentID uuid.UUID, emoji string) ([]Reaction, db.LSN, error) {
	return s.add(ctx, actor, emoji, commentTarget(actor, commentID, true))
}

// UnreactComment takes the caller's emoji off a comment, if it is there.
func (s *Service) UnreactComment(ctx context.Context, actor perm.Actor, commentID uuid.UUID, emoji string) ([]Reaction, db.LSN, error) {
	return s.remove(ctx, actor, emoji, commentTarget(actor, commentID, false))
}

func of(ctx context.Context, tx db.DBTX, viewer uuid.UUID, on target) ([]Reaction, error) {
	if on.comment == nil {
		return OnPage(ctx, tx, viewer, on.page)
	}
	found, err := OnComments(ctx, tx, viewer, []uuid.UUID{*on.comment})
	if err != nil {
		return nil, err
	}
	if list, ok := found[*on.comment]; ok {
		return list, nil
	}
	return []Reaction{}, nil
}

// summarise reads reaction rows, ordered by target, then by each emoji's
// first use, then by when each person reacted, into one list per target.
const summarise = `
WITH r AS (
	SELECT r.comment_id, r.emoji, r.user_id, r.created_at, r.id,
	       min(r.created_at) OVER (PARTITION BY r.comment_id, r.emoji) AS first_at,
	       row_number() OVER (PARTITION BY r.comment_id, r.emoji ORDER BY r.created_at, r.id) AS nth,
	       count(*) OVER (PARTITION BY r.comment_id, r.emoji) AS total
	FROM reaction r
	WHERE %s
)
SELECT r.comment_id, r.emoji, r.total, r.user_id, COALESCE(u.name, ''), r.nth, r.user_id = $1
FROM r LEFT JOIN app_user u ON u.id = r.user_id
WHERE r.nth <= $3 OR r.user_id = $1
ORDER BY r.comment_id, r.first_at, r.emoji, r.nth`

func collect(rows pgx.Rows) (map[uuid.UUID][]Reaction, error) {
	defer rows.Close()
	out := map[uuid.UUID][]Reaction{}
	for rows.Next() {
		var (
			comment *uuid.UUID
			emoji   string
			total   int
			p       Reactor
			nth     int
			mine    bool
		)
		if err := rows.Scan(&comment, &emoji, &total, &p.ID, &p.Name, &nth, &mine); err != nil {
			return nil, err
		}
		key := uuid.Nil
		if comment != nil {
			key = *comment
		}
		list := out[key]
		if n := len(list); n == 0 || list[n-1].Emoji != emoji {
			list = append(list, Reaction{Emoji: emoji, Count: total, People: []Reactor{}})
		}
		last := &list[len(list)-1]
		last.Mine = last.Mine || mine
		if nth <= MaxPeople {
			last.People = append(last.People, p)
		}
		out[key] = list
	}
	return out, rows.Err()
}

// OnPage is the reactions on a page itself, each emoji in the order it was
// first used, as viewer sees them.
func OnPage(ctx context.Context, tx db.DBTX, viewer, pageID uuid.UUID) ([]Reaction, error) {
	rows, err := tx.Query(ctx, fmt.Sprintf(summarise, `r.page_id = $2 AND r.comment_id IS NULL`), viewer, pageID, MaxPeople)
	if err != nil {
		return nil, fmt.Errorf("read the reactions: %w", err)
	}
	found, err := collect(rows)
	if err != nil {
		return nil, err
	}
	if list, ok := found[uuid.Nil]; ok {
		return list, nil
	}
	return []Reaction{}, nil
}

// OnComments is the reactions on each of the comments named, keyed by
// comment; a comment without any is left out.
func OnComments(ctx context.Context, tx db.DBTX, viewer uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]Reaction, error) {
	if len(ids) == 0 {
		return map[uuid.UUID][]Reaction{}, nil
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(summarise, `r.comment_id = ANY($2)`), viewer, ids, MaxPeople)
	if err != nil {
		return nil, fmt.Errorf("read the reactions: %w", err)
	}
	return collect(rows)
}
