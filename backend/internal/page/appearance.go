package page

import (
	"context"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// Width is how wide a page reads: fixed keeps lines short enough to read,
// full gives a wide table or diagram the whole window.
type Width string

const (
	WidthFixed Width = "fixed"
	WidthFull  Width = "full"
)

// Widths are the widths a page may have.
var Widths = []Width{WidthFixed, WidthFull}

// MaxIconRunes bounds an emoji's code points: a family or a flag with
// skin tones and joiners is still one emoji well under it.
const MaxIconRunes = 16

// Appearance is how a page looks, apart from its words: an emoji before its
// title and in the tree, a width, and a cover picture.
type Appearance struct {
	Icon  *string `json:"icon"`
	Width Width   `json:"width"`
	Cover *Cover  `json:"cover"`
}

// Cover is one of the page's own pictures above its title, with the point,
// in percent from the left and the top, that stays in view however it is cut.
type Cover struct {
	AttachmentID uuid.UUID `json:"attachmentId"`
	FocusX       int       `json:"focusX"`
	FocusY       int       `json:"focusY"`
}

// AppearanceInput replaces a page's appearance whole; a null icon or cover
// takes it away.
type AppearanceInput struct {
	Icon  *string `json:"icon"`
	Width Width   `json:"width"`
	Cover *Cover  `json:"cover"`
}

// IsEmoji says whether s is one emoji as a picker gives it: symbols, with
// the joiners, variation selectors, skin tones, keycaps and flag letters
// that make one emoji of several code points; no words and no spaces.
func IsEmoji(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > MaxIconRunes || !utf8.ValidString(s) {
		return false
	}
	symbol, base := false, false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.So, r), unicode.Is(unicode.Sk, r) && r >= 0x1F3FB && r <= 0x1F3FF, r >= 0x1F1E6 && r <= 0x1F1FF:
			symbol = true
		case r == 0x200D, r == 0xFE0F, r == 0xFE0E, r == 0x20E3, r >= 0xE0020 && r <= 0xE007F:
		case r == '#', r == '*', r >= '0' && r <= '9':
			// Keycaps start with one of these and end with U+20E3.
			base = true
		default:
			return false
		}
	}
	if base {
		return containsKeycap(s)
	}
	return symbol
}

func containsKeycap(s string) bool {
	for _, r := range s {
		if r == 0x20E3 {
			return true
		}
	}
	return false
}

// SetAppearance replaces how a page looks. Its cover is one of its own
// pictures; the database refuses any other file, and anybody without edit.
func (s *Service) SetAppearance(ctx context.Context, actor perm.Actor, id uuid.UUID, in AppearanceInput) (*Appearance, db.LSN, error) {
	if in.Icon != nil && !IsEmoji(*in.Icon) {
		return nil, 0, &FieldError{Field: "icon", Message: "Choose one emoji for the page, or none."}
	}
	if in.Width == "" {
		in.Width = WidthFixed
	}
	if in.Width != WidthFixed && in.Width != WidthFull {
		return nil, 0, &FieldError{Field: "width", Message: "Choose fixed or full width."}
	}
	if c := in.Cover; c != nil && (c.FocusX < 0 || c.FocusX > 100 || c.FocusY < 0 || c.FocusY > 100) {
		return nil, 0, &FieldError{Field: "cover", Message: "Put the cover's focus inside the picture."}
	}
	var out *Appearance
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, _, err := load(ctx, tx, actor, id, true)
		if err != nil {
			return err
		}
		if err := current.must(perm.EditPages); err != nil {
			return err
		}
		cover, x, y := (*uuid.UUID)(nil), 50, 50
		if c := in.Cover; c != nil {
			var image bool
			err := tx.QueryRow(ctx, `SELECT width IS NOT NULL FROM attachment WHERE id = $1 AND page_id = $2`, c.AttachmentID, id).Scan(&image)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !image) {
				return &FieldError{Field: "cover", Message: "Choose a picture attached to this page as its cover, or upload one."}
			}
			if err != nil {
				return fmt.Errorf("find the cover: %w", err)
			}
			cover, x, y = &c.AttachmentID, c.FocusX, c.FocusY
		}
		if _, err := tx.Exec(ctx, `UPDATE page SET icon = $2, width = $3, cover_attachment_id = $4, cover_focus_x = $5, cover_focus_y = $6 WHERE id = $1`,
			id, in.Icon, in.Width, cover, x, y); err != nil {
			return fmt.Errorf("change how the page looks: %w", err)
		}
		out = &Appearance{Icon: in.Icon, Width: in.Width}
		if cover != nil {
			out.Cover = &Cover{AttachmentID: *cover, FocusX: x, FocusY: y}
		}
		return nil
	})
	return out, lsn, err
}
