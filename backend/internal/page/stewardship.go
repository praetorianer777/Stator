package page

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

const (
	// MaxVerifyDays is the longest a verification may run, which the
	// database's page_verification_term holds too.
	MaxVerifyDays = 730
	// DefaultVerifyDays is what a verification without a term runs for.
	DefaultVerifyDays = 90
)

// ErrNotStewardable refuses an owner or a verification on a page nobody but
// its author has read yet.
var ErrNotStewardable = errors.New("publish the page before you name its owner or verify it")

// Owner is the person who answers for a page.
type Owner struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// CanView is false once the owner lost access to the page, which then
	// needs another.
	CanView bool `json:"canView"`
}

// VerificationStatus says whether a verification still holds.
type VerificationStatus string

const (
	Verified VerificationStatus = "verified"
	// Expired is a verification whose term ran out; the owner is told.
	Expired VerificationStatus = "expired"
)

// VerificationStatuses lists every VerificationStatus, for the API document.
var VerificationStatuses = []VerificationStatus{Verified, Expired}

// Verification is somebody who may edit a page saying it is right, until a date.
type Verification struct {
	Status VerificationStatus `json:"status"`
	// VerifiedByID is null once the person who verified it is gone.
	VerifiedByID   *uuid.UUID `json:"verifiedById"`
	VerifiedByName string     `json:"verifiedByName"`
	VerifiedAt     time.Time  `json:"verifiedAt"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	// Version is the version the page was at when it was verified.
	Version int `json:"version"`
}

// OwnerInput names a page's owner, a member who may view it.
type OwnerInput struct {
	UserID uuid.UUID `json:"userId"`
}

// VerifyInput verifies a page for Days days, DefaultVerifyDays when zero.
type VerifyInput struct {
	Days int `json:"days,omitempty"`
}

func ownerOf(ctx context.Context, tx db.DBTX, pageID uuid.UUID) (*Owner, error) {
	var o Owner
	err := tx.QueryRow(ctx, `
		SELECT o.user_id, COALESCE(NULLIF(u.name, ''), u.email::text, ''), perm_page_viewable(o.page_id, o.user_id)
		FROM page_owner o LEFT JOIN app_user u ON u.id = o.user_id
		WHERE o.page_id = $1`, pageID).Scan(&o.ID, &o.Name, &o.CanView)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the page's owner: %w", err)
	}
	return &o, nil
}

func verificationOf(ctx context.Context, tx db.DBTX, pageID uuid.UUID) (*Verification, error) {
	var v Verification
	err := tx.QueryRow(ctx, `
		SELECT CASE WHEN v.expires_at > now() THEN 'verified' ELSE 'expired' END,
		       v.verified_by, COALESCE(NULLIF(u.name, ''), u.email::text, ''), v.verified_at, v.expires_at, v.version
		FROM page_verification v LEFT JOIN app_user u ON u.id = v.verified_by
		WHERE v.page_id = $1`, pageID).Scan(&v.Status, &v.VerifiedByID, &v.VerifiedByName, &v.VerifiedAt, &v.ExpiresAt, &v.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the page's verification: %w", err)
	}
	return &v, nil
}

// VerifiedAmong says which of the pages carry a verification that still holds,
// for the lists that badge them.
func VerifiedAmong(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT page_id FROM page_verification WHERE page_id = ANY($1) AND expires_at > now()`, ids)
	if err != nil {
		return nil, fmt.Errorf("read which pages are verified: %w", err)
	}
	verified, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, id := range verified {
		out[id] = true
	}
	return out, nil
}

// steward loads a page for changing its owner or verification, which takes
// edit of a published page.
func steward(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*Page, string, error) {
	p, sp, err := load(ctx, tx, actor, id, true)
	if err != nil {
		return nil, "", err
	}
	if err := p.must(perm.EditPages); err != nil {
		return nil, "", err
	}
	if p.Unpublished {
		return nil, "", ErrNotStewardable
	}
	return p, sp.Key, nil
}

// SetOwner names the person who answers for a page, replacing any before.
func (s *Service) SetOwner(ctx context.Context, actor perm.Actor, id uuid.UUID, in OwnerInput) (*Owner, db.LSN, error) {
	var out *Owner
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, spaceKey, err := steward(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		var (
			name    string
			canView bool
		)
		err = tx.QueryRow(ctx, `
			SELECT COALESCE(NULLIF(u.name, ''), u.email::text), perm_page_viewable($1, u.id)
			FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND m.user_id = $2`, id, in.UserID).Scan(&name, &canView)
		if errors.Is(err, pgx.ErrNoRows) {
			return &FieldError{Field: "userId", Message: "That person is not a member of this organization. Pick somebody from the list."}
		}
		if err != nil {
			return err
		}
		if !canView {
			return &FieldError{Field: "userId", Message: fmt.Sprintf("%s cannot view this page. Pick somebody who can, or let them view it first.", name)}
		}
		var before *uuid.UUID
		if prev, err := ownerOf(ctx, tx, id); err != nil {
			return err
		} else if prev != nil {
			before = &prev.ID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_owner (org_id, page_id, user_id, set_by) VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (org_id, page_id) DO UPDATE SET user_id = EXCLUDED.user_id, set_by = EXCLUDED.set_by`,
			id, in.UserID, actor.UserID); err != nil {
			return fmt.Errorf("name the page's owner: %w", err)
		}
		if err := record(ctx, tx, actor, audit.ActionPageOwnerSet, id, map[string]any{
			"space": spaceKey, "title": p.Title, "owner": in.UserID, "ownerName": name, "previousOwner": before}); err != nil {
			return err
		}
		out = &Owner{ID: in.UserID, Name: name, CanView: true}
		return nil
	})
	return out, lsn, err
}

// RemoveOwner leaves a page without an owner; one without is no change.
func (s *Service) RemoveOwner(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, spaceKey, err := steward(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		var gone uuid.UUID
		err = tx.QueryRow(ctx, `DELETE FROM page_owner WHERE page_id = $1 RETURNING user_id`, id).Scan(&gone)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("remove the page's owner: %w", err)
		}
		return record(ctx, tx, actor, audit.ActionPageOwnerRemoved, id, map[string]any{"space": spaceKey, "title": p.Title, "previousOwner": gone})
	})
}

// Verify says the page is right as it stands, for the days asked, replacing
// any verification before.
func (s *Service) Verify(ctx context.Context, actor perm.Actor, id uuid.UUID, in VerifyInput) (*Verification, db.LSN, error) {
	days := in.Days
	if days == 0 {
		days = DefaultVerifyDays
	}
	if days < 1 || days > MaxVerifyDays {
		return nil, 0, &FieldError{Field: "days", Message: fmt.Sprintf("Choose a term between 1 and %d days.", MaxVerifyDays)}
	}
	var out *Verification
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, spaceKey, err := steward(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_verification (org_id, page_id, verified_by, expires_at)
			VALUES (current_org_id(), $1, $2, now() + make_interval(days => $3))
			ON CONFLICT (org_id, page_id) DO UPDATE SET verified_by = EXCLUDED.verified_by, expires_at = EXCLUDED.expires_at`,
			id, actor.UserID, days); err != nil {
			return fmt.Errorf("verify the page: %w", err)
		}
		if out, err = verificationOf(ctx, tx, id); err != nil {
			return err
		}
		return record(ctx, tx, actor, audit.ActionPageVerified, id, map[string]any{
			"space": spaceKey, "title": p.Title, "version": out.Version, "days": days, "expiresAt": out.ExpiresAt})
	})
	return out, lsn, err
}

// Unverify takes a page's verification away; one without is no change.
func (s *Service) Unverify(ctx context.Context, actor perm.Actor, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, spaceKey, err := steward(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM page_verification WHERE page_id = $1`, id)
		if err != nil {
			return fmt.Errorf("take the verification away: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return record(ctx, tx, actor, audit.ActionPageUnverified, id, map[string]any{"space": spaceKey, "title": p.Title})
	})
}
