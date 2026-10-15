package spaceio

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SiteFormat names an archive of a whole organization, and SiteVersion its
// shape; an import reads every version up to its own and refuses a newer one.
const (
	SiteFormat  = "stator.organization"
	SiteVersion = 1
)

// Paths inside an organization's archive, beside manifest.json.
const (
	siteSpacesDir = "spaces/"
	siteThemesDir = "themes/"
	siteLogoPath  = "brand/logo"
	siteAuditPath = "audit.jsonl"
)

// MaxSiteSpaces bounds the spaces of one organization's archive; each space
// is held to the limits of a space's archive besides.
const MaxSiteSpaces = 2000

// DefaultMaxSiteImportBytes is what an uploaded organization archive may weigh.
const DefaultMaxSiteImportBytes int64 = 2 << 30

// SiteManifest is an organization's archive's table of contents: everything
// that is not a space, and where each space's own archive is.
type SiteManifest struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	// ExportedBy is a person of People.
	ExportedBy   string           `json:"exportedBy"`
	Organization SiteInfo         `json:"organization"`
	Settings     SiteSettings     `json:"settings"`
	People       []SitePerson     `json:"people"`
	Groups       []SiteGroup      `json:"groups"`
	Permissions  []Grant          `json:"permissions"`
	SignIn       *SiteSignIn      `json:"signIn"`
	Armature     *SiteArmature    `json:"armature"`
	Brand        SiteBrand        `json:"brand"`
	Themes       []SiteTheme      `json:"themes"`
	PersonThemes []PersonTheme    `json:"personThemes"`
	Templates    []Template       `json:"templates"`
	Webhooks     []SiteWebhook    `json:"webhooks"`
	Preferences  []SitePreference `json:"notificationPreferences"`
	Spaces       []SiteSpace      `json:"spaces"`
	// AuditEntries counts the lines of audit.jsonl.
	AuditEntries int `json:"auditEntries"`
	// NotCarried is what the organization had that the archive leaves out,
	// with why and what to do about it where it lands.
	NotCarried []Omission `json:"notCarried"`
	Counts     SiteCounts `json:"counts"`
}

// SiteInfo names the organization.
type SiteInfo struct {
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"createdAt"`
}

// SiteSettings are the organization's switches, its hub page by space and
// page, and its default theme by the theme's id in Themes.
type SiteSettings struct {
	AnonymousAccess    bool       `json:"anonymousAccess"`
	AnonymousIndexable bool       `json:"anonymousIndexable"`
	PublicLinks        bool       `json:"publicLinks"`
	HubPage            *uuid.UUID `json:"hubPage"`
	HubLanding         bool       `json:"hubLanding"`
	DefaultTheme       *uuid.UUID `json:"defaultTheme"`
}

// SitePerson is a member of the organization; Ref is how every archive in
// it, its spaces' included, names them.
type SitePerson struct {
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

// SiteGroup is a group with its members by ref.
type SiteGroup struct {
	Ref         string   `json:"ref"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	ExternalRef *string  `json:"externalRef"`
	Members     []string `json:"members"`
}

// SiteSignIn is the sign-in provider without its client secret, and the
// organization roles its groups give.
type SiteSignIn struct {
	Issuer       string          `json:"issuer"`
	ClientID     string          `json:"clientId"`
	GroupsClaim  string          `json:"groupsClaim"`
	Scopes       string          `json:"scopes"`
	CreateGroups bool            `json:"createGroups"`
	Enabled      bool            `json:"enabled"`
	GroupRoles   []SiteGroupRole `json:"groupRoles"`
}

// SiteGroupRole is the organization role a group of the provider gives.
type SiteGroupRole struct {
	Group string `json:"group"`
	Role  string `json:"role"`
}

// SiteArmature is where the organization's Armature is, without its webhook secret.
type SiteArmature struct {
	BaseURL string `json:"baseUrl"`
	OrgSlug string `json:"orgSlug"`
}

// SiteBrand is the brand of the exports; the logo's bytes are at brand/logo.
type SiteBrand struct {
	FooterEn string    `json:"footerEn"`
	FooterDe string    `json:"footerDe"`
	Logo     *SiteLogo `json:"logo"`
}

// SiteLogo describes the logo's bytes.
type SiteLogo struct {
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// SiteTheme is one of the organization's themes; each file's bytes are at
// themes/{file id}, and the spec names the files by those ids.
type SiteTheme struct {
	ID     uuid.UUID       `json:"id"`
	Name   string          `json:"name"`
	Owner  string          `json:"owner"`
	Shared bool            `json:"shared"`
	Spec   json.RawMessage `json:"spec"`
	Files  []SiteThemeFile `json:"files"`
}

// SiteThemeFile is one file of a theme.
type SiteThemeFile struct {
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

// SiteWebhook is an outbound webhook without its signing secret.
type SiteWebhook struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Enabled bool     `json:"enabled"`
	Owner   string   `json:"owner"`
	// Version is the envelope version it is registered for; an archive from
	// before it was numbered leaves it out, which means 1.
	Version int `json:"version,omitempty"`
}

// SitePreference is how a person wants to be told of things.
type SitePreference struct {
	Person    string          `json:"person"`
	InApp     json.RawMessage `json:"inApp"`
	Email     json.RawMessage `json:"email"`
	Digest    string          `json:"digest"`
	AutoWatch bool            `json:"autoWatch"`
}

// SiteSpace is one space and where its archive is inside the organization's.
type SiteSpace struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Archive string `json:"archive"`
	Counts  Counts `json:"counts"`
}

// Omission is one kind of thing the archive leaves out.
type Omission struct {
	// Key names it for a reader in another language; What and Why are English.
	Key  string `json:"key"`
	What string `json:"what"`
	// Count is how many of it the organization had, where they are counted.
	Count *int   `json:"count,omitempty"`
	Why   string `json:"why"`
}

// SiteCounts are what the archive holds, for the progress of its import.
type SiteCounts struct {
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

func siteSpacePath(key string) string { return siteSpacesDir + key + ".zip" }

func siteThemeFilePath(id uuid.UUID) string { return siteThemesDir + id.String() }

// CheckSiteManifest refuses a manifest that is not an organization's archive
// of ours, or newer than this Stator reads.
func CheckSiteManifest(m *SiteManifest) error {
	if m.Format != SiteFormat {
		return invalid("This file is not an organization archive of Stator. Export the organization from its settings in Stator, then import that file.")
	}
	if m.Version < 1 || m.Version > SiteVersion {
		return invalid("This archive is of version %d, which this Stator does not read yet (it reads up to version %d). Update Stator, or export the organization from a Stator of the same version.", m.Version, SiteVersion)
	}
	if len(m.Spaces) > MaxSiteSpaces {
		return invalid("The archive holds %d spaces, more than the %d one import takes. Ask whoever runs Stator to split the organization.", len(m.Spaces), MaxSiteSpaces)
	}
	return nil
}
