package spaceio

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OrgFormat names an archive of a whole organization, and OrgVersion its
// shape; an import reads every version up to its own and refuses a newer one.
const (
	OrgFormat  = "stator.organization"
	OrgVersion = 1
)

// Paths inside an organization's archive, beside manifest.json.
const (
	orgSpacesDir = "spaces/"
	orgThemesDir = "themes/"
	orgLogoPath  = "brand/logo"
	orgAuditPath = "audit.jsonl"
)

// MaxOrgSpaces bounds the spaces of one organization's archive; each space
// is held to the limits of a space's archive besides.
const MaxOrgSpaces = 2000

// DefaultMaxOrgImportBytes is what an uploaded organization archive may weigh.
const DefaultMaxOrgImportBytes int64 = 2 << 30

// OrgManifest is an organization's archive's table of contents: everything
// that is not a space, and where each space's own archive is.
type OrgManifest struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	// ExportedBy is a person of People.
	ExportedBy   string          `json:"exportedBy"`
	Organization OrgInfo         `json:"organization"`
	Settings     OrgSettings     `json:"settings"`
	People       []OrgPerson     `json:"people"`
	Groups       []OrgGroup      `json:"groups"`
	Permissions  []Grant         `json:"permissions"`
	SignIn       *OrgSignIn      `json:"signIn"`
	Armature     *OrgArmature    `json:"armature"`
	Brand        OrgBrand        `json:"brand"`
	Themes       []OrgTheme      `json:"themes"`
	PersonThemes []PersonTheme   `json:"personThemes"`
	Templates    []Template      `json:"templates"`
	Webhooks     []OrgWebhook    `json:"webhooks"`
	Preferences  []OrgPreference `json:"notificationPreferences"`
	Spaces       []OrgSpace      `json:"spaces"`
	// AuditEntries counts the lines of audit.jsonl.
	AuditEntries int `json:"auditEntries"`
	// NotCarried is what the organization had that the archive leaves out,
	// with why and what to do about it where it lands.
	NotCarried []Omission `json:"notCarried"`
	Counts     OrgCounts  `json:"counts"`
}

// OrgInfo names the organization.
type OrgInfo struct {
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"createdAt"`
}

// OrgSettings are the organization's switches, its hub page by space and
// page, and its default theme by the theme's id in Themes.
type OrgSettings struct {
	AnonymousAccess    bool       `json:"anonymousAccess"`
	AnonymousIndexable bool       `json:"anonymousIndexable"`
	PublicLinks        bool       `json:"publicLinks"`
	HubPage            *uuid.UUID `json:"hubPage"`
	HubLanding         bool       `json:"hubLanding"`
	DefaultTheme       *uuid.UUID `json:"defaultTheme"`
}

// OrgPerson is a member of the organization; Ref is how every archive in
// it, its spaces' included, names them.
type OrgPerson struct {
	Ref           string  `json:"ref"`
	Name          string  `json:"name"`
	Email         string  `json:"email"`
	Timezone      string  `json:"timezone"`
	Locale        *string `json:"locale"`
	Active        bool    `json:"active"`
	ShowInReaders bool    `json:"showInReaders"`
	Role          string  `json:"role"`
	RoleSource    string  `json:"roleSource"`
	// GuestSpace is the key of the one space a guest was invited to.
	GuestSpace string    `json:"guestSpace,omitempty"`
	JoinedAt   time.Time `json:"joinedAt"`
}

// OrgGroup is a group with its members by ref.
type OrgGroup struct {
	Ref         string   `json:"ref"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	ExternalRef *string  `json:"externalRef"`
	Members     []string `json:"members"`
}

// OrgSignIn is the sign-in provider without its client secret, and the
// organization roles its groups give.
type OrgSignIn struct {
	Issuer       string         `json:"issuer"`
	ClientID     string         `json:"clientId"`
	GroupsClaim  string         `json:"groupsClaim"`
	Scopes       string         `json:"scopes"`
	CreateGroups bool           `json:"createGroups"`
	Enabled      bool           `json:"enabled"`
	GroupRoles   []OrgGroupRole `json:"groupRoles"`
}

// OrgGroupRole is the organization role a group of the provider gives.
type OrgGroupRole struct {
	Group string `json:"group"`
	Role  string `json:"role"`
}

// OrgArmature is where the organization's Armature is, without its webhook secret.
type OrgArmature struct {
	BaseURL string `json:"baseUrl"`
	OrgSlug string `json:"orgSlug"`
}

// OrgBrand is the brand of the exports; the logo's bytes are at brand/logo.
type OrgBrand struct {
	FooterEn string   `json:"footerEn"`
	FooterDe string   `json:"footerDe"`
	Logo     *OrgLogo `json:"logo"`
}

// OrgLogo describes the logo's bytes.
type OrgLogo struct {
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// OrgTheme is one of the organization's themes; each file's bytes are at
// themes/{file id}, and the spec names the files by those ids.
type OrgTheme struct {
	ID     uuid.UUID       `json:"id"`
	Name   string          `json:"name"`
	Owner  string          `json:"owner"`
	Shared bool            `json:"shared"`
	Spec   json.RawMessage `json:"spec"`
	Files  []OrgThemeFile  `json:"files"`
}

// OrgThemeFile is one file of a theme.
type OrgThemeFile struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
}

// PersonTheme is the theme a person chose for themselves.
type PersonTheme struct {
	Person         string     `json:"person"`
	Theme          *uuid.UUID `json:"theme"`
	FollowArmature bool       `json:"followArmature"`
}

// OrgWebhook is an outbound webhook without its signing secret.
type OrgWebhook struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Enabled bool     `json:"enabled"`
	Owner   string   `json:"owner"`
}

// OrgPreference is how a person wants to be told of things.
type OrgPreference struct {
	Person    string          `json:"person"`
	InApp     json.RawMessage `json:"inApp"`
	Email     json.RawMessage `json:"email"`
	Digest    string          `json:"digest"`
	AutoWatch bool            `json:"autoWatch"`
}

// OrgSpace is one space and where its archive is inside the organization's.
type OrgSpace struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Archive string `json:"archive"`
	Counts  Counts `json:"counts"`
}

// Omission is one kind of thing the archive leaves out.
type Omission struct {
	What string `json:"what"`
	// Count is how many of it the organization had, where they are counted.
	Count *int   `json:"count,omitempty"`
	Why   string `json:"why"`
}

// OrgCounts are what the archive holds, for the progress of its import.
type OrgCounts struct {
	Spaces   int `json:"spaces"`
	Pages    int `json:"pages"`
	Versions int `json:"versions"`
	Files    int `json:"files"`
	Comments int `json:"comments"`
	Drafts   int `json:"drafts"`
	People   int `json:"people"`
	Groups   int `json:"groups"`
}

// AuditEntry is one line of audit.jsonl, its actor by ref.
type AuditEntry struct {
	Actor      string          `json:"actor,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   *uuid.UUID      `json:"targetId"`
	Data       json.RawMessage `json:"data"`
	At         time.Time       `json:"at"`
}

func orgSpacePath(key string) string { return orgSpacesDir + key + ".zip" }

func orgThemeFilePath(id uuid.UUID) string { return orgThemesDir + id.String() }

// CheckOrgManifest refuses a manifest that is not an organization's archive
// of ours, or newer than this Stator reads.
func CheckOrgManifest(m *OrgManifest) error {
	if m.Format != OrgFormat {
		return invalid("This file is not an organization archive of Stator. Export the organization from its settings in Stator, then import that file.")
	}
	if m.Version < 1 || m.Version > OrgVersion {
		return invalid("This archive is of version %d, which this Stator does not read yet (it reads up to version %d). Update Stator, or export the organization from a Stator of the same version.", m.Version, OrgVersion)
	}
	if len(m.Spaces) > MaxOrgSpaces {
		return invalid("The archive holds %d spaces, more than the %d one import takes. Ask whoever runs Stator to split the organization.", len(m.Spaces), MaxOrgSpaces)
	}
	return nil
}
