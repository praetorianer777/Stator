package page

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// SeedLease is how long the person asked to send a new shared draft's first
// content has before the next person to open it is asked instead.
const SeedLease = 15 * time.Second

var (
	// ErrRoomGone refuses a change to a shared draft that was thrown away or
	// started afresh since its sender joined it.
	ErrRoomGone = errors.New("the shared draft was started afresh; open it again")
	// ErrRoomSeeded refuses first content for a shared draft that has some.
	ErrRoomSeeded = errors.New("somebody else began the shared draft first; open it again")
	// ErrCompactStale refuses a compaction of updates that changed meanwhile.
	ErrCompactStale = errors.New("the shared draft changed while it was being compacted")
	// ErrCollabRefused is the database refusing a change to the shared draft,
	// as it does once its sender may no longer edit the page.
	ErrCollabRefused = errors.New("you may no longer edit this page, so your changes are not shared")
	// ErrBadBase refuses a shared draft said to start from a version the page never had.
	ErrBadBase = errors.New("a shared draft starts from a version the page has had")
)

// The database's codes for a row level security refusal and a missing room.
const (
	insufficientPrivilege = "42501"
	foreignKeyViolation   = "23503"
)

// CollabRoom is a page's shared draft as one person opens it: which life of
// it, the version it was begun from, and every update it holds.
type CollabRoom struct {
	ID     uuid.UUID
	PageID uuid.UUID
	Base   int
	// Seed asks the opener to send the first content, from their own draft
	// or the page.
	Seed    bool
	Updates []CollabUpdate
	// Replaced is the life this open ended, whose editors start again.
	Replaced *uuid.UUID
}

// CollabUpdate is one update of a shared draft, numbered in the order the
// database took it.
type CollabUpdate struct {
	Seq  int64
	Body []byte
}

// collabRefusal reads the database's refusals of a write to the shared draft.
func collabRefusal(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case insufficientPrivilege:
			return ErrCollabRefused
		case foreignKeyViolation:
			return ErrRoomGone
		}
	}
	return err
}

// OpenCollab opens the page's shared draft for somebody who may edit it,
// beginning one when there is none, and starting it afresh when a publish
// from elsewhere moved the page on and the draft holds nothing unpublished.
func (s *Service) OpenCollab(ctx context.Context, actor perm.Actor, id uuid.UUID) (*CollabRoom, error) {
	var out *CollabRoom
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Locked, so two people opening at once see one room between them.
		p, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := p.must(perm.EditPages); err != nil {
			return err
		}
		if p.Kind == KindFolder {
			return ErrFolder
		}
		room := &CollabRoom{PageID: id}
		var (
			seeded    bool
			seedUntil *time.Time
			through   int64
			last      int64
			now       time.Time
		)
		err = tx.QueryRow(ctx, `
			SELECT c.id, c.base_version, c.seeded, c.seed_until, c.published_through,
			       COALESCE((SELECT max(u.seq) FROM page_collab_update u WHERE u.room_id = c.id), 0), now()
			FROM page_collab c WHERE c.page_id = $1 FOR UPDATE`, id).
			Scan(&room.ID, &room.Base, &seeded, &seedUntil, &through, &last, &now)
		fresh := errors.Is(err, pgx.ErrNoRows)
		if err != nil && !fresh {
			return err
		}
		if !fresh && room.Base < p.Version && last <= through {
			old := room.ID
			if _, err := tx.Exec(ctx, `DELETE FROM page_collab WHERE page_id = $1`, id); err != nil {
				return fmt.Errorf("start the shared draft afresh: %w", err)
			}
			room.Replaced, fresh = &old, true
		}
		if fresh {
			if err := tx.QueryRow(ctx, `
				INSERT INTO page_collab (org_id, page_id, base_version) VALUES (current_org_id(), $1, $2)
				RETURNING id, now()`, id, p.Version).Scan(&room.ID, &now); err != nil {
				return fmt.Errorf("begin the shared draft: %w", err)
			}
			room.Base, seeded, seedUntil = p.Version, false, nil
		}
		if !seeded && (seedUntil == nil || seedUntil.Before(now)) {
			if _, err := tx.Exec(ctx, `UPDATE page_collab SET seed_until = now() + $2 * interval '1 millisecond' WHERE id = $1`,
				room.ID, SeedLease.Milliseconds()); err != nil {
				return err
			}
			room.Seed = true
		}
		room.Updates, err = collabUpdates(ctx, tx, room.ID)
		out = room
		return err
	})
	return out, err
}

func collabUpdates(ctx context.Context, tx db.DBTX, room uuid.UUID) ([]CollabUpdate, error) {
	rows, err := tx.Query(ctx, `SELECT seq, body FROM page_collab_update WHERE room_id = $1 ORDER BY seq`, room)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CollabUpdate])
}

// CheckCollab says whether the actor may still edit the page, as a shared
// draft asks every so often of everybody in it.
func (s *Service) CheckCollab(ctx context.Context, actor perm.Actor, id uuid.UUID) error {
	return s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, _, err := load(ctx, tx, actor, id, false)
		if err != nil {
			return err
		}
		return p.must(perm.EditPages)
	})
}

// ReloadCollab is a shared draft's base and every update it holds, read on
// the primary, where the latest one is.
func (s *Service) ReloadCollab(ctx context.Context, actor perm.Actor, id, room uuid.UUID) (int, []CollabUpdate, error) {
	var (
		base int
		out  []CollabUpdate
	)
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT base_version FROM page_collab WHERE page_id = $1 AND id = $2`, id, room).Scan(&base)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoomGone
		}
		if err != nil {
			return err
		}
		out, err = collabUpdates(ctx, tx, room)
		return err
	})
	return base, out, err
}

// CountCollab is how many updates a shared draft holds.
func (s *Service) CountCollab(ctx context.Context, actor perm.Actor, room uuid.UUID) (int, error) {
	var n int
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM page_collab_update WHERE room_id = $1`, room).Scan(&n)
	})
	return n, err
}

// AppendCollab adds an update to a shared draft and says where it went. The
// database refuses it once the sender may no longer edit the page.
func (s *Service) AppendCollab(ctx context.Context, actor perm.Actor, id, room uuid.UUID, body []byte) (int64, error) {
	var seq int64
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			INSERT INTO page_collab_update (org_id, page_id, room_id, body) VALUES (current_org_id(), $1, $2, $3)
			RETURNING seq`, id, room, body).Scan(&seq)
	})
	return seq, collabRefusal(err)
}

// SeedCollab writes a shared draft's first content, begun from base, which
// only the person asked may send and only while it has none.
func (s *Service) SeedCollab(ctx context.Context, actor perm.Actor, id, room uuid.UUID, base int, body []byte) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			seeded  bool
			version int
		)
		err := tx.QueryRow(ctx, `
			SELECT c.seeded, p.version FROM page_collab c JOIN page p ON p.id = c.page_id
			WHERE c.page_id = $1 AND c.id = $2 FOR UPDATE OF c`, id, room).Scan(&seeded, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoomGone
		}
		if err != nil {
			return err
		}
		if seeded {
			return ErrRoomSeeded
		}
		if base < 0 || base > version {
			return ErrBadBase
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_collab_update (org_id, page_id, room_id, body) VALUES (current_org_id(), $1, $2, $3)`,
			id, room, body); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE page_collab SET seeded = true, seed_until = NULL, base_version = $2 WHERE id = $1`, room, base)
		return err
	})
	return collabRefusal(err)
}

// PublishedCollab moves a shared draft on to the version the actor just
// published from it, and notes how far the draft had come, so a room left
// with nothing new can be started afresh later. It answers the base.
func (s *Service) PublishedCollab(ctx context.Context, actor perm.Actor, id, room uuid.UUID, version int) (int, error) {
	var base int
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT base_version FROM page_collab WHERE page_id = $1 AND id = $2 FOR UPDATE`, id, room).Scan(&base)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoomGone
		}
		if err != nil {
			return err
		}
		var mine bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM page_version WHERE page_id = $1 AND number = $2 AND created_by = $3)`,
			id, version, actor.UserID).Scan(&mine); err != nil {
			return err
		}
		if !mine || version <= base {
			return ErrBadBase
		}
		base = version
		_, err = tx.Exec(ctx, `
			UPDATE page_collab SET base_version = $2,
			       published_through = COALESCE((SELECT max(seq) FROM page_collab_update WHERE room_id = $1), 0)
			WHERE id = $1`, room, version)
		return err
	})
	return base, collabRefusal(err)
}

// DiscardCollab throws a shared draft away, for everybody in it.
func (s *Service) DiscardCollab(ctx context.Context, actor perm.Actor, id, room uuid.UUID) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM page_collab WHERE page_id = $1 AND id = $2`, id, room)
		return err
	})
	return collabRefusal(err)
}

// CompactCollab replaces the count updates numbered from through to with
// their merge, under the last of their numbers, or changes nothing when they
// are not all still there.
func (s *Service) CompactCollab(ctx context.Context, actor perm.Actor, id, room uuid.UUID, from, to int64, count int, merged []byte) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM page_collab_update WHERE room_id = $1 AND seq BETWEEN $2 AND $3`, room, from, to)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != int64(count) {
			return ErrCompactStale
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO page_collab_update (org_id, page_id, room_id, seq, body) VALUES (current_org_id(), $1, $2, $3, $4)`,
			id, room, to, merged)
		return err
	})
	return collabRefusal(err)
}
