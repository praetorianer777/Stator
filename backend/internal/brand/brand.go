// Package brand is what an organization's exports carry besides its theme:
// a logo and a line for the footer, in English and German. Administrators
// change them; every member reads them, and so does an export.
package brand

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// MaxLogoBytes bounds the logo; an export carries it once per file.
	MaxLogoBytes = 2 << 20
	// MaxFooterLength bounds a footer line, in characters.
	MaxFooterLength = 200
)

var (
	// ErrNotAdmin refuses a change of the brand by anybody but an administrator.
	ErrNotAdmin = errors.New("only an administrator of the organization changes its brand")
	// ErrBadLogoType is a file that is no PNG, JPEG or WebP picture.
	ErrBadLogoType = errors.New("a logo is a PNG, JPEG or WebP picture")
	// ErrLogoTooLarge is a logo over the limit.
	ErrLogoTooLarge = errors.New("that logo is too large: it is up to 2 MB")
	// ErrNoLogo answers a read of a logo the organization has not set.
	ErrNoLogo = errors.New("the organization has no logo")
)

// FieldError refuses one field of a change, in a sentence the form shows under it.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Logo describes the picture; Version changes with each upload.
type Logo struct {
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Version     int    `json:"version"`
}

// Footer is the line an export prints at its foot, in each language.
type Footer struct {
	En string `json:"en"`
	De string `json:"de"`
}

// In picks the line for a language, English where the language has none.
func (f Footer) In(language string) string {
	if strings.HasPrefix(strings.ToLower(language), "de") && f.De != "" {
		return f.De
	}
	return f.En
}

// Brand is the organization's brand. Logo is null when there is none.
type Brand struct {
	Logo   *Logo  `json:"logo"`
	Footer Footer `json:"footer"`
}

// FooterInput replaces both footer lines.
type FooterInput struct {
	En string `json:"en"`
	De string `json:"de"`
}

type Service struct {
	db    *db.Cluster
	store objectstore.Store
}

func NewService(cluster *db.Cluster, store objectstore.Store) *Service {
	return &Service{db: cluster, store: store}
}

// Read is the brand in the caller's transaction; an organization that never
// set one has an empty brand.
func Read(ctx context.Context, tx db.DBTX) (*Brand, error) {
	var (
		out     Brand
		kind    *string
		size    *int
		version int
	)
	err := tx.QueryRow(ctx, `SELECT footer_en, footer_de, logo_type, logo_size, logo_version FROM org_brand WHERE org_id = current_org_id()`).
		Scan(&out.Footer.En, &out.Footer.De, &kind, &size, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return &out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the brand: %w", err)
	}
	if kind != nil && size != nil {
		out.Logo = &Logo{ContentType: *kind, Size: *size, Version: version}
	}
	return &out, nil
}

// Get is the organization's brand.
func (s *Service) Get(ctx context.Context) (*Brand, error) {
	var out *Brand
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = Read(ctx, tx)
		return err
	})
	return out, err
}

func requireAdmin(ctx context.Context, tx db.DBTX, actor perm.Actor) error {
	f, err := perm.LoadFacts(ctx, tx, actor, uuid.Nil)
	if err != nil {
		return err
	}
	if !f.OrgAdmin() {
		return ErrNotAdmin
	}
	return nil
}

// SetFooter replaces the footer lines.
func (s *Service) SetFooter(ctx context.Context, actor perm.Actor, in FooterInput) (*Brand, db.LSN, error) {
	in.En, in.De = strings.TrimSpace(in.En), strings.TrimSpace(in.De)
	for field, line := range map[string]string{"en": in.En, "de": in.De} {
		if strings.ContainsAny(line, "\r\n") {
			return nil, 0, &FieldError{Field: field, Message: "A footer is one line. Take out the line break."}
		}
		if utf8.RuneCountInString(line) > MaxFooterLength {
			return nil, 0, &FieldError{Field: field, Message: fmt.Sprintf("A footer is at most %d characters. Shorten it.", MaxFooterLength)}
		}
	}
	var out *Brand
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_brand (org_id, footer_en, footer_de, updated_by) VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (org_id) DO UPDATE SET footer_en = $1, footer_de = $2, updated_by = $3, updated_at = now()`,
			in.En, in.De, actor.UserID); err != nil {
			return fmt.Errorf("save the footer: %w", err)
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionOrgBrandSet, "org", nil, map[string]any{"part": "footer"}); err != nil {
			return err
		}
		var err error
		out, err = Read(ctx, tx)
		return err
	})
	return out, lsn, err
}

func sniffLogo(data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrBadLogoType
	}
	if len(data) > MaxLogoBytes {
		return "", ErrLogoTooLarge
	}
	switch kind := http.DetectContentType(data); kind {
	case "image/png", "image/jpeg", "image/webp":
		return kind, nil
	}
	return "", ErrBadLogoType
}

// LogoKey is where one version of the logo is in the object store.
func LogoKey(orgID uuid.UUID, version int) string {
	return fmt.Sprintf("%sbrand/logo-%d", objectstore.OrgPrefix(orgID), version)
}

// SetLogo replaces the logo. The type is what the bytes say.
func (s *Service) SetLogo(ctx context.Context, actor perm.Actor, body io.Reader) (*Brand, db.LSN, error) {
	if objectstore.IsUnavailable(s.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxLogoBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read upload: %w", err)
	}
	kind, err := sniffLogo(data)
	if err != nil {
		return nil, 0, err
	}
	org, _ := tenant.FromContext(ctx)
	var (
		out *Brand
		old *Logo
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRow(ctx, `
			INSERT INTO org_brand (org_id, logo_type, logo_size, logo_version, updated_by) VALUES (current_org_id(), $1, $2, 1, $3)
			ON CONFLICT (org_id) DO UPDATE SET logo_type = $1, logo_size = $2, logo_version = org_brand.logo_version + 1, updated_by = $3, updated_at = now()
			RETURNING logo_version`, kind, len(data), actor.UserID).Scan(&version); err != nil {
			return fmt.Errorf("save the logo: %w", err)
		}
		if err := s.store.Put(ctx, LogoKey(org.ID, version), bytes.NewReader(data), int64(len(data)), kind); err != nil {
			return err
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionOrgBrandSet, "org", nil, map[string]any{"part": "logo"}); err != nil {
			return err
		}
		var err error
		out, err = Read(ctx, tx)
		if err == nil && version > 1 {
			old = &Logo{Version: version - 1}
		}
		return err
	})
	if err == nil && old != nil {
		// The previous picture is no longer named by anything.
		_ = s.store.Delete(ctx, LogoKey(org.ID, old.Version))
	}
	return out, lsn, err
}

// DeleteLogo takes the logo away; exports then carry the name alone.
func (s *Service) DeleteLogo(ctx context.Context, actor perm.Actor) (*Brand, db.LSN, error) {
	org, _ := tenant.FromContext(ctx)
	var (
		out     *Brand
		version int
		had     bool
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			UPDATE org_brand SET logo_type = NULL, logo_size = NULL, updated_by = $1, updated_at = now()
			WHERE org_id = current_org_id() AND logo_type IS NOT NULL RETURNING logo_version`, actor.UserID).Scan(&version)
		had = err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("take the logo away: %w", err)
		}
		if had {
			if err := perm.Record(ctx, tx, actor, audit.ActionOrgBrandSet, "org", nil, map[string]any{"part": "logo removed"}); err != nil {
				return err
			}
		}
		out, err = Read(ctx, tx)
		return err
	})
	if err == nil && had {
		_ = s.store.Delete(ctx, LogoKey(org.ID, version))
	}
	return out, lsn, err
}

// OpenLogo reads the logo for a member, or an export. The caller closes it.
// The row is read as the application: a public site shows the logo of an
// organization to a reader who cannot read the organization's tables.
func (s *Service) OpenLogo(ctx context.Context) (io.ReadCloser, *Logo, error) {
	org, _ := tenant.FromContext(ctx)
	var logo *Logo
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			kind    *string
			size    *int
			version int
		)
		err := tx.QueryRow(ctx, `SELECT logo_type, logo_size, logo_version FROM org_brand WHERE org_id = $1`, org.ID).Scan(&kind, &size, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the logo: %w", err)
		}
		if kind != nil && size != nil {
			logo = &Logo{ContentType: *kind, Size: *size, Version: version}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if logo == nil {
		return nil, nil, ErrNoLogo
	}
	body, err := s.store.Get(ctx, LogoKey(org.ID, logo.Version))
	if err != nil {
		return nil, nil, ErrNoLogo
	}
	return body, logo, nil
}
