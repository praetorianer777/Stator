package spaceio

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/brand"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

// htmlBrand is what an HTML export carries of the organization's brand: its
// name over every page, its logo, its footer line, and its theme's accent
// colour in the style sheet. A page of the export is English, so is the line,
// unless only the German one is written.
type htmlBrand struct {
	name, footer string
	// light and dark are the accent colours of the default theme, empty where it names none.
	light, dark string
	logoKey     string
	logoExt     string
}

const brandLogoPath = htmlFiles + "brand/logo."

var accentHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

var logoExtensions = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}

// readHTMLBrand reads the brand in the export's transaction.
func readHTMLBrand(ctx context.Context, tx db.DBTX) (*htmlBrand, error) {
	b, err := brand.Read(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := &htmlBrand{footer: strings.TrimSpace(b.Footer.En)}
	if out.footer == "" {
		out.footer = strings.TrimSpace(b.Footer.De)
	}
	var (
		light, dark *string
		orgID       uuid.UUID
	)
	// The id comes from the row: a worker's context need not carry the tenant.
	if err := tx.QueryRow(ctx, `
		SELECT o.id, o.name, t.spec->'colors'->'light'->>'accent', t.spec->'colors'->'dark'->>'accent'
		FROM org o LEFT JOIN theme t ON t.id = o.default_theme_id AND t.org_id = o.id
		WHERE o.id = current_org_id()`).Scan(&orgID, &out.name, &light, &dark); err != nil {
		return nil, fmt.Errorf("read the organization for the export: %w", err)
	}
	if light != nil && accentHex.MatchString(*light) {
		out.light = *light
	}
	if dark != nil && accentHex.MatchString(*dark) {
		out.dark = *dark
	}
	if b.Logo != nil {
		if ext, ok := logoExtensions[b.Logo.ContentType]; ok {
			out.logoKey, out.logoExt = brand.LogoKey(orgID, b.Logo.Version), ext
		}
	}
	return out, nil
}

// style is the style sheet with the organization's accent in place of the
// built-in one; light first, and for a reader who prefers dark its dark accent,
// else its light one.
func (b *htmlBrand) style(base []byte) []byte {
	if b == nil || (b.light == "" && b.dark == "") {
		return base
	}
	var out strings.Builder
	out.Write(base)
	light := b.light
	dark := b.dark
	if light == "" {
		light = dark
	}
	if dark == "" {
		dark = light
	}
	out.WriteString("\n/* The organization's accent, from its default theme. */\n")
	out.WriteString(":root { --accent: " + light + "; }\n")
	out.WriteString("@media (prefers-color-scheme: dark) { :root { --accent: " + dark + "; } }\n")
	return []byte(out.String())
}

// logoPath is where the logo is written, or empty where there is none.
func (b *htmlBrand) logoPath() string {
	if b == nil || b.logoKey == "" {
		return ""
	}
	return brandLogoPath + b.logoExt
}

// readLogo reads the logo's bytes; an export is not stopped by a logo that is gone.
func (b *htmlBrand) readLogo(ctx context.Context, store objectstore.Store) []byte {
	if b == nil || b.logoKey == "" || objectstore.IsUnavailable(store) {
		return nil
	}
	body, err := store.Get(ctx, b.logoKey)
	if err != nil {
		return nil
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, brand.MaxLogoBytes+1))
	if err != nil || len(data) > brand.MaxLogoBytes {
		return nil
	}
	return data
}
