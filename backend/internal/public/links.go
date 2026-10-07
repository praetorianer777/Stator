package public

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

const (
	// MaxLinksPerPage is how many live public links one page may have; the
	// database's page_link_max() is the same number.
	MaxLinksPerPage = 5
	// MaxLinkLabelLength bounds the label that tells a page's links apart.
	MaxLinkLabelLength = 60
	// LinkPathSegment is the part of a public address after the organization
	// that a link's token follows, in the web client as in the api.
	LinkPathSegment = "link"
)

// tokenShape is what auth.GenerateToken makes: 32 bytes, base64url without
// padding. Anything else is no token, and costs no query.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// ErrLinkGone answers every link that opens nothing alike, whatever the
// reason: a reader cannot tell which, and needs a new link for any of them.
var ErrLinkGone = errors.New("this link does not open anything any more")

// ErrLinkNotFound answers revoking a link the page does not have, or has no
// longer, since somebody revoked it first.
var ErrLinkNotFound = errors.New("public link not found")

// Refusal says why no link can be made for a page now.
type Refusal string

const (
	// RefusalCannotManage: the person may not change the page, or is a guest.
	RefusalCannotManage Refusal = "cannotManage"
	// RefusalOff: the organization allows no public links.
	RefusalOff Refusal = "off"
	// RefusalPersonal: the page is in a personal space, which is never public.
	RefusalPersonal Refusal = "personal"
	// RefusalFolder: a folder only lists its pages, which a link does not open.
	RefusalFolder Refusal = "folder"
	// RefusalTrashed: the page is in the trash.
	RefusalTrashed Refusal = "trashed"
	// RefusalUnpublished: the page, or a page above it, is not published yet.
	RefusalUnpublished Refusal = "unpublished"
	// RefusalRestricted: a view list on the page or above it names who may read it.
	RefusalRestricted Refusal = "restricted"
	// RefusalFull: the page has MaxLinksPerPage live links already.
	RefusalFull Refusal = "full"
)

// Refusals lists every refusal, for the document's enum.
var Refusals = []Refusal{RefusalCannotManage, RefusalOff, RefusalPersonal, RefusalFolder, RefusalTrashed, RefusalUnpublished, RefusalRestricted, RefusalFull}

// RefusedError refuses making or revoking a link, in a sentence that says
// what to do about it.
type RefusedError struct {
	Reason Refusal
}

func (e *RefusedError) Error() string {
	switch e.Reason {
	case RefusalCannotManage:
		return "Only people who may edit this page make public links for it and revoke them. Ask one of its editors."
	case RefusalOff:
		return "Your organization does not allow public links. Ask an administrator of the organization to turn them on under Settings, Permissions."
	case RefusalPersonal:
		return "A page of a personal space is never opened to anybody. Move it to a team space first, then make the link."
	case RefusalFolder:
		return "A folder only lists the pages in it, and a link opens one page. Make a link for a page inside the folder instead."
	case RefusalTrashed:
		return "This page is in the trash. Restore it first, then make the link."
	case RefusalUnpublished:
		return "This page, or a page above it, has not been published yet. Publish it first, then make the link."
	case RefusalRestricted:
		return "This page, or a page above it, is restricted to some people, and a public link would open it to anybody. Lift the restriction first, or share the page with those people instead."
	case RefusalFull:
		return fmt.Sprintf("This page already has %d public links, which is as many as a page may have. Revoke one first.", MaxLinksPerPage)
	}
	return "No public link can be made for this page now. Reload the page and try again."
}

// FieldError is a refusal of one field of a link asked for.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// LinkSettings is whether the organization allows public links at all.
type LinkSettings struct {
	// Enabled lets the editors of a page make links that anybody may read it
	// through; while it is off every link stops working and none can be made.
	Enabled bool `json:"enabled"`
}

// Link is a live public link of a page, as its editors see it. The token is
// shown once, when the link is made, and never again.
type Link struct {
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
	// CreatedBy is who made the link, or null once they left the organization.
	CreatedBy *perm.Person `json:"createdBy"`
	CreatedAt time.Time    `json:"createdAt"`
	// ExpiresAt is when the link stops working by itself, or null for never.
	ExpiresAt *time.Time `json:"expiresAt"`
}

// PageLinks is what the share dialog shows of a page's public links.
type PageLinks struct {
	// Links are the live links, oldest first; empty for somebody who may not
	// manage them.
	Links []Link `json:"links"`
	Max   int    `json:"max"`
	// Refusal says why no link can be made now, or is null when one can.
	Refusal *Refusal `json:"refusal"`
}

// LinkInput asks for a link, with an optional label and expiry.
type LinkInput struct {
	Label string `json:"label,omitempty"`
	// ExpiresAt is when the link stops working; omitted, it works until revoked.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Clean checks an input against now and returns its label trimmed.
func (in LinkInput) Clean(now time.Time) (string, error) {
	label := strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(label) > MaxLinkLabelLength {
		return "", &FieldError{Field: "label", Message: fmt.Sprintf("Keep the label to %d characters; it only tells this page's links apart.", MaxLinkLabelLength)}
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return "", &FieldError{Field: "expiresAt", Message: "Choose an expiry that is still ahead, or none for a link that works until it is revoked."}
	}
	return label, nil
}

// LinkPath is the web client's address of a link: the organization, then
// the token, so the address names whom it reads in as #79's public pages do.
func LinkPath(orgSlug, token string) string {
	return "/public/" + url.PathEscape(orgSlug) + "/" + LinkPathSegment + "/" + url.PathEscape(token)
}

// IsToken says whether a string has the shape of a link's token.
func IsToken(s string) bool { return tokenShape.MatchString(s) }

// LinkedPage is a page read through a public link: its words with every
// person in them unnamed, and nothing of its space, tree or people.
type LinkedPage struct {
	ID         uuid.UUID       `json:"id"`
	Title      string          `json:"title"`
	Kind       page.Kind       `json:"kind"`
	Appearance page.Appearance `json:"appearance"`
	// Body is the published document with its mentions unnamed.
	Body    json.RawMessage `json:"body"`
	Version int             `json:"version"`
	Updated time.Time       `json:"updatedAt"`
}
