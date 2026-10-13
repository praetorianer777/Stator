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
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/perm"
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
	key         string
}

// Key is where the picture is in the object store, as the row stores it: a
// logo set before the organization left the schema keeps its old key.
func (l *Logo) Key() string { return l.key }

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
		key     *string
	)
	err := tx.QueryRow(ctx, `SELECT footer_en, footer_de, logo_type, logo_size, logo_version, logo_key FROM brand`).
		Scan(&out.Footer.En, &out.Footer.De, &kind, &size, &version, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return &out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the brand: %w", err)
	}
	out.Logo = logoOf(kind, size, version, key)
	return &out, nil
}

func logoOf(kind *string, size *int, version int, key *string) *Logo {
	if kind == nil || size == nil || key == nil {
		return nil
	}
	return &Logo{ContentType: *kind, Size: *size, Version: version, key: *key}
}

// previousLogo is the key of the logo there is now, locked so a change
// beside it waits, or "" without one.
func previousLogo(ctx context.Context, tx db.DBTX) (string, error) {
	var key *string
	err := tx.QueryRow(ctx, `SELECT logo_key FROM brand FOR UPDATE`).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the logo: %w", err)
	}
	if key == nil {
		return "", nil
	}
	return *key, nil
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
			INSERT INTO brand (footer_en, footer_de, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (singleton) DO UPDATE SET footer_en = $1, footer_de = $2, updated_by = $3, updated_at = now()`,
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
	var (
		out      *Brand
		previous string
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		var err error
		if previous, err = previousLogo(ctx, tx); err != nil {
			return err
		}
		var key string
		if err := tx.QueryRow(ctx, `
			INSERT INTO brand (logo_type, logo_size, logo_version, updated_by) VALUES ($1, $2, 1, $3)
			ON CONFLICT (singleton) DO UPDATE SET logo_type = $1, logo_size = $2, logo_version = brand.logo_version + 1, updated_by = $3, updated_at = now()
			RETURNING logo_key`, kind, len(data), actor.UserID).Scan(&key); err != nil {
			return fmt.Errorf("save the logo: %w", err)
		}
		if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), kind); err != nil {
			return err
		}
		if err := perm.Record(ctx, tx, actor, audit.ActionOrgBrandSet, "org", nil, map[string]any{"part": "logo"}); err != nil {
			return err
		}
		out, err = Read(ctx, tx)
		return err
	})
	if err == nil && previous != "" {
		// The previous picture is no longer named by anything.
		_ = s.store.Delete(ctx, previous)
	}
	return out, lsn, err
}

// DeleteLogo takes the logo away; exports then carry the name alone.
func (s *Service) DeleteLogo(ctx context.Context, actor perm.Actor) (*Brand, db.LSN, error) {
	var (
		out      *Brand
		previous string
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := requireAdmin(ctx, tx, actor); err != nil {
			return err
		}
		var err error
		if previous, err = previousLogo(ctx, tx); err != nil {
			return err
		}
		if previous != "" {
			if _, err := tx.Exec(ctx, `
				UPDATE brand SET logo_type = NULL, logo_size = NULL, updated_by = $1, updated_at = now()`, actor.UserID); err != nil {
				return fmt.Errorf("take the logo away: %w", err)
			}
			if err := perm.Record(ctx, tx, actor, audit.ActionOrgBrandSet, "org", nil, map[string]any{"part": "logo removed"}); err != nil {
				return err
			}
		}
		out, err = Read(ctx, tx)
		return err
	})
	if err == nil && previous != "" {
		_ = s.store.Delete(ctx, previous)
	}
	return out, lsn, err
}

// OpenLogo reads the logo for a member, or an export. The caller closes it.
// The row is read as the application: a public site shows the logo of an
// organization to a reader who cannot read the organization's tables.
func (s *Service) OpenLogo(ctx context.Context) (io.ReadCloser, *Logo, error) {
	var logo *Logo
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		b, err := Read(ctx, tx)
		if err != nil {
			return err
		}
		logo = b.Logo
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if logo == nil {
		return nil, nil, ErrNoLogo
	}
	body, err := s.store.Get(ctx, logo.key)
	if err != nil {
		return nil, nil, ErrNoLogo
	}
	return body, logo, nil
}

// Export is what an export carries of the brand: the organization's name, its
// footer line in the reader's language, its logo's bytes if it has one, and
// the accent colour of its default theme.
type Export struct {
	Name   string
	Footer string
	Accent string
	Logo   []byte
}

var accentColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Export reads the brand for a document made in language, as whoever the
// context is; an export is not stopped by a logo that cannot be read.
func (s *Service) Export(ctx context.Context, language string) (*Export, error) {
	var (
		out    Export
		footer Footer
		logo   *Logo
		accent *string
	)
	// As the application's own read, not the reader's: an anonymous reader
	// cannot read the organization's row, and the brand is public to every
	// export of the organization whoever asks.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var kind, key *string
		var size *int
		var version int
		err := tx.QueryRow(ctx, `
			SELECT s.name, t.spec->'colors'->'light'->>'accent',
			       COALESCE(b.footer_en, ''), COALESCE(b.footer_de, ''), b.logo_type, b.logo_size, COALESCE(b.logo_version, 0), b.logo_key
			FROM site s
			LEFT JOIN theme t ON t.id = s.default_theme_id
			LEFT JOIN brand b ON true`).Scan(&out.Name, &accent, &footer.En, &footer.De, &kind, &size, &version, &key)
		if err != nil {
			return err
		}
		logo = logoOf(kind, size, version, key)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the brand for an export: %w", err)
	}
	out.Footer = footer.In(language)
	if accent != nil && accentColour.MatchString(*accent) {
		out.Accent = *accent
	}
	if logo != nil && !objectstore.IsUnavailable(s.store) {
		if body, err := s.store.Get(ctx, logo.key); err == nil {
			out.Logo, _ = io.ReadAll(io.LimitReader(body, MaxLogoBytes+1))
			_ = body.Close()
			if len(out.Logo) > MaxLogoBytes {
				out.Logo = nil
			}
		}
	}
	return &out, nil
}
