// Package testinstance arranges the instance's site for the browser suite:
// people of a spec's own, signed in without a provider, and the site's
// settings put back between the specs that change them.
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
	// ErrNoOrganization is returned before the seed has made the site.
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
	Role     auth.Role
	SpaceKey string
}

// Person is a test person as the endpoint returns them.
type Person struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  auth.Role `json:"role"`
}

// Session is a person with the session opened for them.
type Session struct {
	Person
	SessionID uuid.UUID
	Secret    string
	ExpiresAt time.Time
	LSN       db.LSN
}

// Service works on the instance's site.
type Service struct {
	db         *db.Cluster
	sso        *oidc.Service
	people     seed.People
	sessionTTL time.Duration
}

func NewService(cluster *db.Cluster, sso *oidc.Service, people seed.People, sessionTTL time.Duration) *Service {
	return &Service{db: cluster, sso: sso, people: people, sessionTTL: sessionTTL}
}

// CreatePerson makes a member with the role asked for
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
		if err := s.exists(ctx, tx); err != nil {
			return err
		}
		var space *uuid.UUID
		if in.Role == auth.RoleGuest {
			var id uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM space WHERE key = $1`, in.SpaceKey).Scan(&id)
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
		standing := auth.RoleMember
		if in.Role == auth.RoleGuest {
			standing = auth.RoleGuest
		}
		if _, err := tx.Exec(ctx, `INSERT INTO member (user_id, role, guest_space_id) VALUES ($1, $2, $3)`,
			out.ID, string(standing), space); err != nil {
			return fmt.Errorf("make the membership: %w", err)
		}
		if in.Role == auth.RoleAdmin {
			if err := auth.GrantAdministrator(ctx, tx, out.ID, nil); err != nil {
				return err
			}
		}
		if space != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, space_id, user_id) VALUES ($1, $2, $3)`,
				string(guest.RoleViewer), *space, out.ID); err != nil {
				return fmt.Errorf("let the guest in: %w", err)
			}
		}
		var err error
		out.SessionID, out.Secret, err = auth.OpenSession(ctx, tx, out.ID, auth.ProofPassword, out.ExpiresAt, "", "")
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

// Reset puts back what belongs to the site as a whole as the bootstrap left
// it, so no spec sees another's settings. People, spaces and the audit log
// stay.
func (s *Service) Reset(ctx context.Context) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := s.exists(ctx, tx); err != nil {
			return err
		}
		for i, stmt := range resetStatements {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("reset the site, statement %d: %w", i+1, err)
			}
		}
		// The bootstrap's administrators get theirs back before anybody else's
		// grant across the installation goes, so one stays throughout.
		for _, m := range s.people.Members {
			if m.Role != auth.RoleAdmin && m.Role != "owner" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO role_assignment (role, user_id)
				SELECT $2, u.id FROM app_user u JOIN member m ON m.user_id = u.id WHERE u.email = $1
				ON CONFLICT DO NOTHING`,
				auth.NormalizeEmail(m.Email), auth.AdministratorRole); err != nil {
				return fmt.Errorf("give %s their bootstrap role back: %w", m.Email, err)
			}
		}
		bootstrap := make([]string, 0, len(s.people.Members))
		for _, m := range s.people.Members {
			if m.Role == auth.RoleAdmin || m.Role == "owner" {
				bootstrap = append(bootstrap, auth.NormalizeEmail(m.Email))
			}
		}
		// Test people keep theirs: each was made with the role its spec asked for.
		if _, err := tx.Exec(ctx, `
			DELETE FROM role_assignment a
			WHERE a.space_id IS NULL
			  AND NOT (a.role = $1 AND a.user_id IN (SELECT id FROM app_user WHERE email = ANY ($2) OR email LIKE $3))
			  AND NOT (a.role = 'member' AND a.group_id = (SELECT id FROM groups WHERE builtin = 'everyone'))`,
			auth.AdministratorRole, bootstrap, "%@"+PeopleDomain); err != nil {
			return fmt.Errorf("take back the grants across the site: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_assignment (role, group_id)
			SELECT 'member', id FROM groups WHERE builtin = 'everyone'
			ON CONFLICT DO NOTHING`); err != nil {
			return fmt.Errorf("let every member use the site again: %w", err)
		}
		// Roles back as the migration left them; one a space still grants
		// stays, since spaces stay.
		if _, err := tx.Exec(ctx, `
			DELETE FROM role r WHERE NOT r.builtin AND r.key NOT IN ('member', 'space_creator')
			  AND NOT EXISTS (SELECT 1 FROM role_assignment a WHERE a.role = r.key)`); err != nil {
			return fmt.Errorf("take back the roles specs added: %w", err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE role r SET name = d.name, description = d.description, permissions = d.permissions
			FROM (VALUES
			    ('administrator', 'Administrator', 'Administers the installation: its people, roles, groups and settings, and every space in it.',
			     ARRAY['view', 'addComments', 'addPages', 'delete', 'administer', 'use', 'createSpace', 'administerSite']),
			    ('space_administrator', 'Space administrator', 'Administers a space: its details, permissions, trash and archive, and works in it like an editor.',
			     ARRAY['view', 'addComments', 'addPages', 'delete', 'administer']),
			    ('editor', 'Editor', 'Adds, edits and moves pages, comments, and moves pages to the trash and back.',
			     ARRAY['view', 'addComments', 'addPages', 'delete']),
			    ('commenter', 'Commenter', 'Reads a space and comments on its pages.', ARRAY['view', 'addComments']),
			    ('viewer', 'Viewer', 'Reads a space and changes nothing in it.', ARRAY['view']),
			    ('member', 'Member', 'Uses Stator: signs in and reaches what the grants on spaces open.', ARRAY['use']),
			    ('space_creator', 'Space creator', 'Creates spaces, together with a role that lets them use Stator.', ARRAY['createSpace'])
			) AS d (key, name, description, permissions)
			WHERE r.key = d.key AND (r.name, r.description, r.permissions) IS DISTINCT FROM (d.name, d.description, d.permissions)`)
		if err != nil {
			return fmt.Errorf("put the roles back: %w", err)
		}
		return nil
	})
	if err != nil || s.people.Provider == nil {
		return err
	}
	if err := s.sso.Configure(ctx, *s.people.Provider); err != nil {
		return fmt.Errorf("point the site at the bootstrap provider again: %w", err)
	}
	return nil
}

// resetStatements run in order: the hub and the default theme are cleared
// before the pages and themes they could point at go.
var resetStatements = []string{
	`UPDATE site SET settings = '{}', anonymous_access = false, anonymous_indexable = false, public_links = true,
	        hub_landing = false, hub_page_id = NULL, default_theme_id = NULL`,
	`DELETE FROM brand`,
	`DELETE FROM armature_connection`,
	`DELETE FROM exciter_connection`,
	`DELETE FROM webhook_endpoint`,
	`DELETE FROM page_template WHERE space_id IS NULL`,
	`DELETE FROM example_job`,
	`DELETE FROM space WHERE example`,
	`DELETE FROM join_request`,
}

// AgeExample makes the example space one made before its content had
// versions (#320), and without the page of the file drop, so a renewal has a
// page to add.
func (s *Service) AgeExample(ctx context.Context, spaceKey, drop string) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := s.exists(ctx, tx); err != nil {
			return err
		}
		var space, dropped uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT s.id, p.page_id FROM space s
			JOIN example_page p ON p.space_id = s.id AND p.name = $2 AND p.page_id IS NOT NULL
			WHERE s.key = $1 AND s.example`,
			spaceKey, drop).Scan(&space, &dropped)
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

func (s *Service) exists(ctx context.Context, tx db.DBTX) error {
	_, err := instance.Find(ctx, tx)
	if errors.Is(err, instance.ErrNotBootstrapped) {
		return ErrNoOrganization
	}
	return err
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
