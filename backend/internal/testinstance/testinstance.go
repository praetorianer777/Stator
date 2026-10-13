// Package testinstance arranges the instance's one organization for the
// browser suite: people of a spec's own, signed in without a provider, and
// the organization's settings put back between the specs that change them.
// The api serves it only when STATOR_TEST_ENDPOINTS is on, which no
// production deployment sets.
package testinstance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/guest"
	"github.com/praetorianer777/stator/backend/internal/instance"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/seed"
)

var (
	// ErrNotFound is returned for a person this package did not make, which
	// keeps a mistyped id from removing a real one.
	ErrNotFound = errors.New("no test person has that id")
	// ErrNoExample is returned when the space has no example page of that name.
	ErrNoExample = errors.New("that space is no example space with that page")
	// ErrBadRole is returned for a role a test person cannot have.
	ErrBadRole = errors.New("choose admin, member or guest")
	// ErrGuestSpace is returned when a guest names no space it can be let
	// into, or somebody else names one.
	ErrGuestSpace = errors.New("a guest needs the key of an existing space, and nobody else takes one")
	// ErrNoOrganization is returned before the seed has made the organization.
	ErrNoOrganization = errors.New("the organization does not exist yet; run the seed")
)

// PeopleDomain is where every test person's address is. Only a person at it
// can be removed through this package.
const PeopleDomain = "people.stator.test"

// Address limits: the local part keeps the label readable, and the random
// suffix keeps two runs of one spec apart.
const (
	maxLabelLength = 40
	suffixBytes    = 4
	defaultLabel   = "person"
)

// NewPerson is who a spec asks for. Label starts the address, so a leftover
// person names the spec that made them; Name is what the interface shows.
type NewPerson struct {
	Label    string
	Name     string
	Role     auth.OrgRole
	SpaceKey string
}

// Person is a test person as the endpoint returns them.
type Person struct {
	ID    uuid.UUID    `json:"id"`
	Email string       `json:"email"`
	Name  string       `json:"name"`
	Role  auth.OrgRole `json:"role"`
}

// Session is a person with the session opened for them.
type Session struct {
	Person
	SessionID uuid.UUID
	Secret    string
	ExpiresAt time.Time
	LSN       db.LSN
}

// Service works on the instance's organization.
type Service struct {
	db         *db.Cluster
	sso        *oidc.Service
	people     seed.People
	sessionTTL time.Duration
}

func NewService(cluster *db.Cluster, sso *oidc.Service, people seed.People, sessionTTL time.Duration) *Service {
	return &Service{db: cluster, sso: sso, people: people, sessionTTL: sessionTTL}
}

// CreatePerson makes a person in the organization with the role asked for
// and opens a session for them, as a password sign-in would.
func (s *Service) CreatePerson(ctx context.Context, in NewPerson) (*Session, error) {
	switch in.Role {
	case auth.RoleAdmin, auth.RoleMember:
		if in.SpaceKey != "" {
			return nil, ErrGuestSpace
		}
	case auth.RoleGuest:
		if in.SpaceKey == "" {
			return nil, ErrGuestSpace
		}
	default:
		return nil, ErrBadRole
	}
	suffix := make([]byte, suffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return nil, err
	}
	label := localPart(in.Label)
	out := Session{Person: Person{
		Email: label + "-" + hex.EncodeToString(suffix) + "@" + PeopleDomain,
		Name:  strings.TrimSpace(in.Name),
		Role:  in.Role,
	}}
	if out.Name == "" {
		out.Name = label
	}
	out.ExpiresAt = time.Now().Add(s.sessionTTL)
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		orgID, err := s.orgID(ctx, tx)
		if err != nil {
			return err
		}
		var space *uuid.UUID
		if in.Role == auth.RoleGuest {
			var id uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM space WHERE org_id = $1 AND key = $2`, orgID, in.SpaceKey).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrGuestSpace
			}
			if err != nil {
				return err
			}
			space = &id
		}
		if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`, out.Email, out.Name).Scan(&out.ID); err != nil {
			return fmt.Errorf("make the person: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role, guest_space_id) VALUES ($1, $2, $3, $4)`,
			orgID, out.ID, string(in.Role), space); err != nil {
			return fmt.Errorf("make the membership: %w", err)
		}
		if space != nil {
			for _, p := range guest.RoleViewer.Permissions() {
				if _, err := tx.Exec(ctx, `INSERT INTO space_grant (org_id, space_id, permission, subject_type, user_id) VALUES ($1, $2, $3, 'user', $4)`,
					orgID, *space, string(p), out.ID); err != nil {
					return fmt.Errorf("let the guest in: %w", err)
				}
			}
		}
		out.SessionID, out.Secret, err = auth.OpenSession(ctx, tx, out.ID, &orgID, auth.ProofPassword, out.ExpiresAt, "", "")
		return err
	})
	if err != nil {
		return nil, err
	}
	out.LSN = lsn
	return &out, nil
}

// DeletePerson removes a test person, the spaces they made and so everything
// in them, which is how a spec's leftovers go when its worker stops.
func (s *Service) DeletePerson(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `SELECT 1 FROM app_user WHERE id = $1 AND email LIKE $2 FOR UPDATE`, id, "%@"+PeopleDomain)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM space WHERE created_by = $1`, id); err != nil {
			return fmt.Errorf("remove the person's spaces: %w", err)
		}
		_, err = tx.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, id)
		return err
	})
	return err
}

// Reset puts back what belongs to the organization as a whole as the
// bootstrap left it, so no spec sees another's settings. People, spaces and
// the audit log stay.
func (s *Service) Reset(ctx context.Context) error {
	var org instance.Org
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if org, err = instance.Find(ctx, tx); err != nil {
			return err
		}
		id := org.ID
		for i, stmt := range resetStatements {
			if _, err := tx.Exec(ctx, stmt, id); err != nil {
				return fmt.Errorf("reset the organization, statement %d: %w", i+1, err)
			}
		}
		for _, m := range s.people.Members {
			if _, err := tx.Exec(ctx, `
				UPDATE org_member SET org_role = $3, role_source = 'manual'
				WHERE org_id = $1 AND user_id = (SELECT id FROM app_user WHERE email = $2)
				  AND (org_role, role_source) IS DISTINCT FROM ($3, 'manual')`,
				id, auth.NormalizeEmail(m.Email), string(m.Role)); err != nil {
				return fmt.Errorf("give %s their bootstrap role back: %w", m.Email, err)
			}
		}
		return nil
	})
	if err != nil || s.people.Provider == nil {
		return err
	}
	if err := s.sso.Configure(ctx, org.ID, *s.people.Provider); err != nil {
		return fmt.Errorf("point the organization at the bootstrap provider again: %w", err)
	}
	return nil
}

// resetStatements each take the organization's id. The hub and the default
// theme are cleared before the pages and themes they could point at go.
var resetStatements = []string{
	`UPDATE org SET settings = '{}', anonymous_access = false, anonymous_indexable = false, public_links = true,
	        hub_landing = false, hub_page_id = NULL, default_theme_id = NULL WHERE id = $1`,
	`DELETE FROM global_grant WHERE org_id = $1`,
	`INSERT INTO global_grant (org_id, permission, subject_type) VALUES ($1, 'use', 'everyone')`,
	`DELETE FROM org_brand WHERE org_id = $1`,
	`DELETE FROM oidc_group_role WHERE org_id = $1`,
	`DELETE FROM armature_connection WHERE org_id = $1`,
	`DELETE FROM webhook_endpoint WHERE org_id = $1`,
	`DELETE FROM page_template WHERE org_id = $1 AND space_id IS NULL`,
	`DELETE FROM example_job WHERE org_id = $1`,
	`DELETE FROM space WHERE org_id = $1 AND example`,
	`DELETE FROM org_join_request WHERE org_id = $1`,
}

// AgeExample makes the example space one made before its content had
// versions (#320), and without the page of the file drop, so a renewal has a
// page to add.
func (s *Service) AgeExample(ctx context.Context, spaceKey, drop string) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		orgID, err := s.orgID(ctx, tx)
		if err != nil {
			return err
		}
		var space, dropped uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT s.id, p.page_id FROM space s
			JOIN example_page p ON p.space_id = s.id AND p.name = $3 AND p.page_id IS NOT NULL
			WHERE s.org_id = $1 AND s.key = $2 AND s.example`,
			orgID, spaceKey, drop).Scan(&space, &dropped)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoExample
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM example_page WHERE space_id = $1`, space); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM page WHERE id = $1`, dropped); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE space SET example_version = 0 WHERE id = $1`, space)
		return err
	})
	return err
}

func (s *Service) orgID(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
	org, err := instance.Find(ctx, tx)
	if errors.Is(err, instance.ErrNone) {
		return uuid.Nil, ErrNoOrganization
	}
	return org.ID, err
}

// localPart keeps what an address's local part takes, so any label works.
func localPart(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if b.Len() >= maxLabelLength {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return defaultLabel
	}
	return out
}
