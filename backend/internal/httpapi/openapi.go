package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/calendar"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/home"
	"github.com/praetorianer777/stator/backend/internal/hub"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/mdio"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/openapi"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/pageview"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/share"
	"github.com/praetorianer777/stator/backend/internal/shortcut"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/stale"
	"github.com/praetorianer777/stator/backend/internal/star"
	"github.com/praetorianer777/stator/backend/internal/task"
	"github.com/praetorianer777/stator/backend/internal/template"
	"github.com/praetorianer777/stator/backend/internal/theme"
	"github.com/praetorianer777/stator/backend/internal/unfurl"
	"github.com/praetorianer777/stator/backend/internal/watch"
	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// The API described in terms of the code that serves it. Every route in Routes
// has a row in operations, and a test refuses a router and a table that
// disagree; the document is then derived from the rows by reflection.

// env is a response envelope: the one or two keys a handler wraps its result
// in. A nil value is an untyped JSON value; a typed nil pointer is nullable.
type env map[string]any

// param is a query parameter.
type param struct {
	name, description string
	schema            *openapi.Schema
	repeated          bool
}

// operation is one row of the table.
type operation struct {
	method, path string
	handler      string
	// id names the operation for clients when the handler's name would not
	// be unique, as when one handler serves two paths.
	id      string
	summary string
	tag     string
	query   []param
	// request is the JSON body type the handler decodes; multipart says the
	// body is a file upload in a part named file instead.
	request   any
	multipart bool
	// responses maps a status to its envelope or bare type; nil is no body.
	responses map[int]any
	// public routes need no session; binary ones answer with a file.
	public bool
	binary bool
	// redirect routes answer with a Location header rather than a body.
	redirect bool
	// pending routes are agreed but not built yet: they answer 501, and the
	// integration suite does not expect them covered. See docs/architecture.md.
	pending bool
	// raw says any JSON is accepted, as rawNote describes it.
	raw     bool
	rawNote string
	// tool names the operation for an assistant and toolHelp is the sentence
	// the model reads; empty means it is not offered (see mcp_test.go for why).
	tool, toolHelp string
	// toolFile is the name a multipart tool sends its content under when the
	// call names none.
	toolFile string
	// orgWide routes concern the organization as a whole, which a token
	// limited to spaces is refused; token_test.go holds the router to it.
	orgWide bool
}

// operations is the table. Order is by area, then by path; paths are relative
// to APIPrefix except the probes, which live at the root.
var operations = []operation{
	{method: "GET", path: "/healthz", handler: "handleLiveness", tag: "health", summary: "Whether the process is running.", public: true,
		responses: ok(statusResponse{})},
	{method: "GET", path: "/readyz", handler: "handleReadiness", tag: "health", summary: "Whether the process can serve traffic, and how reads are routed.", public: true,
		responses: map[int]any{200: readinessResponse{}, 503: statusResponse{}}},
	{method: "GET", path: "/openapi.json", handler: "handleOpenAPI", tag: "health", summary: "This document.", public: true,
		responses: ok(openapi.Document{})},

	{method: "POST", path: "/auth/login", handler: "handleLogin", tag: "auth", summary: "Sign in a local account with email and password, and set the session cookie.", public: true,
		request: loginRequest{}, responses: ok(meResponse{})},
	{method: "GET", path: "/auth/oidc/{orgSlug}/start", handler: "handleOIDCStart", tag: "auth", summary: "Begin signing in through the organization's identity provider.", public: true, redirect: true,
		query: []param{{name: "next", description: "A path of this application to land on afterwards."}}, responses: map[int]any{}},
	{method: "GET", path: "/auth/oidc/callback", handler: "handleOIDCCallback", tag: "auth", summary: "Where the identity provider sends the browser back; sets the session cookie.", public: true, redirect: true,
		query: []param{{name: "state"}, {name: "code"}, {name: "error"}}, responses: map[int]any{}},
	{method: "POST", path: "/auth/logout", handler: "handleLogout", tag: "auth", summary: "End the session.", responses: none()},
	{method: "GET", path: "/auth/me", handler: "handleMe", tool: "whoami", toolHelp: "Who the token belongs to and which organization it acts in.", tag: "auth", summary: "Who is signed in, and the organizations they may act in.",
		responses: ok(meResponse{})},
	{method: "PATCH", path: "/auth/me", handler: "handleUpdateMe", tag: "auth", summary: "Change the caller's own settings, such as the language the interface speaks to them.",
		request: updateMeRequest{}, responses: map[int]any{200: meResponse{}, 422: errorEnvelope{}}},
	{method: "POST", path: "/auth/switch-org", handler: "handleSwitchOrg", tag: "auth", summary: "Move the session to another organization.",
		request: switchOrgRequest{}, responses: ok(env{"organization": auth.CurrentOrg{}})},

	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", orgWide: true, tag: "access", summary: "The organization's identity provider, if one is configured. For administrators.",
		responses: ok(providerView{})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", orgWide: true, tag: "access", summary: "Configure the organization's identity provider. For administrators.",
		request: saveOIDCProviderRequest{}, responses: ok(providerView{})},
	{method: "GET", path: "/oidc-provider/group-roles", handler: "handleListGroupRoles", orgWide: true, tag: "access", summary: "Which provider groups grant which role. For administrators.",
		responses: ok(env{"groupRoles": []oidc.GroupRole{}})},
	{method: "POST", path: "/oidc-provider/group-roles", handler: "handleSetGroupRole", orgWide: true, tag: "access", summary: "Map a provider group to a role, or change the role it maps to; members follow at their next sign-in. For administrators.",
		request: setGroupRoleRequest{}, responses: ok(env{"groupRole": oidc.GroupRole{}})},
	{method: "DELETE", path: "/oidc-provider/group-roles/{groupRoleID}", handler: "handleRemoveGroupRole", orgWide: true, tag: "access", summary: "Unmap a provider group; the roles it granted go at each person's next sign-in. For administrators.",
		responses: none()},
	{method: "GET", path: "/users", handler: "handleListMembers", orgWide: true, tag: "access", summary: "The organization's members, with their roles and whether the identity provider decides them. For administrators.",
		responses: ok(env{"members": []auth.Member{}})},
	{method: "DELETE", path: "/users/{userID}", handler: "handleRemoveMember", orgWide: true, tag: "access", summary: "Take somebody out of the organization. The owner stays. For administrators.",
		responses: none()},
	{method: "GET", path: "/users/requests", handler: "handleListJoinRequests", orgWide: true, tag: "access", summary: "Who signed in through the identity provider and is waiting to be let in. For administrators.",
		responses: ok(env{"requests": []auth.JoinRequest{}})},
	{method: "POST", path: "/users/requests/{userID}/admit", handler: "handleAdmitJoinRequest", orgWide: true, tag: "access", summary: "Let a waiting person in with the standing given. For administrators.",
		request: admitRequest{}, responses: ok(env{"membership": auth.Membership{}})},
	{method: "DELETE", path: "/users/requests/{userID}", handler: "handleDeclineJoinRequest", orgWide: true, tag: "access", summary: "Turn a waiting person away; they may ask again. For administrators.",
		responses: none()},
	{method: "GET", path: "/org/tokens", handler: "handleListOrgAPITokens", orgWide: true, tag: "access", summary: "Every personal access token in the organization, with whose it is. For administrators.",
		responses: ok(env{"tokens": []auth.OrgAPIToken{}})},
	{method: "DELETE", path: "/org/tokens/{tokenID}", handler: "handleRevokeOrgAPIToken", orgWide: true, tag: "access", summary: "Revoke anybody's token in the organization. For administrators.",
		responses: none()},

	{method: "GET", path: "/tokens", handler: "handleListAPITokens", orgWide: true, tag: "tokens", summary: "The caller's personal access tokens in this organization, without their secrets.",
		responses: ok(env{"tokens": []auth.APIToken{}})},
	{method: "POST", path: "/tokens", handler: "handleCreateAPIToken", orgWide: true, tag: "tokens", summary: "Make a personal access token; the secret is in this answer and never again. Needs a session.",
		request: createTokenRequest{}, responses: created(env{"token": auth.APIToken{}})},
	{method: "DELETE", path: "/tokens/{tokenID}", handler: "handleRevokeAPIToken", orgWide: true, tag: "tokens", summary: "Revoke one of the caller's tokens; it stops working at once.",
		responses: none()},

	// Themes, as Armature serves them.
	{method: "GET", path: "/themes", handler: "handleListThemes", tag: "themes", summary: "Themes the caller may use: theirs, then the shared ones.", responses: ok(env{"themes": []theme.Theme{}})},
	{method: "POST", path: "/themes", handler: "handleCreateTheme", tag: "themes", summary: "Make a theme.", request: theme.Input{}, responses: created(env{"theme": theme.Theme{}})},
	{method: "GET", path: "/themes/examples", handler: "handleThemeExamples", tag: "themes", summary: "The themes shipped with the product, to start a theme from.", responses: ok(env{"examples": []theme.Example{}})},
	{method: "GET", path: "/themes/active", handler: "handleActiveTheme", tag: "themes", summary: "The theme the caller sees: chosen, followed from Armature, the organization's default, or null for the built-in one.", responses: ok(env{"theme": (*theme.Theme)(nil), "source": ""})},
	{method: "PUT", path: "/themes/active", handler: "handleChooseTheme", tag: "themes", summary: "Use a theme; null returns to the organization's default, null with builtIn keeps the built-in one.", request: chooseThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "PUT", path: "/themes/default", handler: "handleSetDefaultTheme", tag: "themes", summary: "Name the shared theme everybody sees until they choose, or null for the built-in one.", request: defaultThemeRequest{}, responses: ok(env{"theme": (*theme.Theme)(nil)})},
	{method: "POST", path: "/themes/import", handler: "handleImportTheme", tag: "themes", summary: "Make a theme of the caller's own from an exported theme file, sent as a multipart part named file.", multipart: true, responses: created(env{"theme": theme.Theme{}})},
	{method: "GET", path: "/themes/{themeID}/export", handler: "handleExportTheme", tag: "themes", summary: "The theme as one file, its pictures and fonts inside, as a download.", binary: true, responses: ok(nil)},
	{method: "GET", path: "/themes/{themeID}", handler: "handleGetTheme", tag: "themes", summary: "One theme.", responses: ok(env{"theme": theme.Theme{}})},
	{method: "PATCH", path: "/themes/{themeID}", handler: "handleUpdateTheme", tag: "themes", summary: "Change a theme; the owner's to do, or an administrator's once shared.", request: theme.Input{}, responses: ok(env{"theme": theme.Theme{}})},
	{method: "DELETE", path: "/themes/{themeID}", handler: "handleDeleteTheme", tag: "themes", summary: "Delete a theme; everybody using it returns to the built-in one.", responses: none()},
	{method: "POST", path: "/themes/{themeID}/assets", handler: "handleUploadThemeAsset", tag: "themes", summary: "Put a picture or a font on a theme, as a multipart part named file.", multipart: true, responses: created(env{"asset": theme.Asset{}})},
	{method: "GET", path: "/themes/{themeID}/assets/{assetID}", handler: "handleThemeAsset", tag: "themes", summary: "The bytes of a theme's file, as a download.", binary: true, responses: ok(nil)},
	{method: "DELETE", path: "/themes/{themeID}/assets/{assetID}", handler: "handleDeleteThemeAsset", tag: "themes", summary: "Take a file off a theme it no longer uses.", responses: none()},

	{method: "GET", path: "/spaces", handler: "handleListSpaces", tool: "list_spaces", toolHelp: "The spaces the caller may see, with the keys other tools take; archived true lists archived ones too.", tag: "spaces", summary: "Every space the caller may see, by name; archived ones only when asked for.",
		query:     []param{{name: "archived", schema: &openapi.Schema{Type: "boolean"}, description: "true to list archived spaces too; false when absent."}},
		responses: ok(env{"spaces": []space.Space{}})},
	{method: "POST", path: "/spaces", handler: "handleCreateSpace", orgWide: true, tag: "spaces", summary: "Make a space and its home page. For whoever may create spaces; with personal, everybody makes their own one, which only they see.", request: space.CreateInput{}, responses: created(env{"space": space.Space{}})},
	{method: "GET", path: "/spaces/{spaceKey}", handler: "handleGetSpace", tool: "get_space", toolHelp: "One space by its key, with its home page id and what the caller may do in it.", tag: "spaces", summary: "One space by its key, and what the caller may do in it.", responses: ok(env{"space": space.Space{}})},
	{method: "PATCH", path: "/spaces/{spaceKey}", handler: "handleUpdateSpace", tag: "spaces", summary: "Rename or describe a space. For the space's administrators.", request: space.UpdateInput{}, responses: ok(env{"space": space.Space{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}", handler: "handleDeleteSpace", tag: "spaces", summary: "Delete a space and every page in it. For the space's administrators.", responses: none()},

	{method: "GET", path: "/spaces/{spaceKey}/pages", handler: "handleListPages", tool: "list_child_pages", toolHelp: "The pages directly under a parent page, in order; without parent, those under the space's home page.", tag: "pages", summary: "The pages directly under a parent, by default under the space's home page, in order.",
		query: []param{{name: "parent", description: "The page whose children to list.", schema: &openapi.Schema{Type: "string", Format: "uuid"}}}, responses: ok(env{"pages": []page.TreeNode{}})},
	{method: "GET", path: "/spaces/{spaceKey}/outline", handler: "handleSpaceOutline", tool: "get_space_outline", toolHelp: "Every page of a space in reading order with its depth, to find a page or where a new one goes.", tag: "pages", summary: "Every page of a space in reading order, with its depth, for choosing where a page goes.", responses: ok(env{"pages": []page.OutlineEntry{}})},
	{method: "GET", path: "/spaces/{spaceKey}/decisions", handler: "handleListDecisions", tool: "list_decisions", toolHelp: "The decision items on a space's published pages, newest page first, each with its state and its page; state decided or undecided keeps one kind.", tag: "pages", summary: "The decision log of a space: every decision item on its published pages the caller may read, newest page first.",
		query:     []param{{name: "state", schema: &openapi.Schema{Type: "string", Enum: []string{"decided", "undecided"}}, description: "decided or undecided to keep one state; both when absent."}},
		responses: ok(page.DecisionLog{})},
	// Archive (#37).
	{method: "PUT", path: "/spaces/{spaceKey}/archive", handler: "handleArchiveSpace", tag: "archive", summary: "Archive a space: it stays readable, leaves the space list, search and the home page, and none of its pages changes. For the space's administrators; archiving it again is no change.",
		responses: ok(env{"space": space.Space{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}/archive", handler: "handleUnarchiveSpace", tag: "archive", summary: "Unarchive a space; one that is not archived is no change. For the space's administrators.",
		responses: ok(env{"space": space.Space{}})},
	{method: "GET", path: "/spaces/{spaceKey}/archived-pages", handler: "handleListArchivedPages", tool: "list_archived_pages", toolHelp: "The pages archived in a space that the caller may view; they stay readable with get_page and get_page_markdown.", tag: "archive", summary: "The space's archive: each archived page the caller may view, with the pages archived with it counted, the latest first.",
		responses: ok(env{"items": []page.ArchiveItem{}})},
	{method: "PUT", path: "/pages/{pageID}/archive", handler: "handleArchivePage", tag: "archive", summary: "Archive a page with every page below it: they stay readable, leave the tree, search and the home page, and none of them changes. For the space's administrators; archiving it again is no change.",
		responses: ok(env{"page": page.Page{}})},
	{method: "DELETE", path: "/pages/{pageID}/archive", handler: "handleUnarchivePage", tag: "archive", summary: "Unarchive a page and the pages archived with it; refused for a page archived with one above it. For the space's administrators.",
		responses: ok(env{"page": page.Page{}})},
	// Shortcuts (#39).
	{method: "GET", path: "/spaces/{spaceKey}/shortcuts", handler: "handleListShortcuts", tool: "list_space_shortcuts", toolHelp: "The links a space pins above its page tree, in order: pages the caller may view and addresses on the web.", tag: "shortcuts",
		summary:   "The space's shortcuts in order: each a page or an address. A shortcut to a page the caller may not view, or one in the trash, is left out.",
		responses: ok(env{"shortcuts": []shortcut.Shortcut{}})},
	{method: "POST", path: "/spaces/{spaceKey}/shortcuts", handler: "handleCreateShortcut", tag: "shortcuts",
		summary: "Pin a shortcut last: a page the caller may view, or an http or https address with a label, its host when none is given. For the space's administrators.",
		request: shortcut.ShortcutInput{}, responses: map[int]any{201: env{"shortcut": shortcut.Shortcut{}}, 409: errorEnvelope{}, 422: errorEnvelope{}}},
	{method: "POST", path: "/spaces/{spaceKey}/shortcuts/{shortcutID}/move", handler: "handleMoveShortcut", tag: "shortcuts",
		summary: "Put a shortcut after another of the space, or first when after is null, and answer them all in their new order. For the space's administrators.",
		request: shortcut.ShortcutMove{}, responses: ok(env{"shortcuts": []shortcut.Shortcut{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}/shortcuts/{shortcutID}", handler: "handleDeleteShortcut", tag: "shortcuts",
		summary: "Remove a shortcut; the page it opened stays. For the space's administrators.", responses: none()},

	// Team calendars (#60).
	{method: "GET", path: "/spaces/{spaceKey}/calendars", handler: "handleListCalendars", tool: "list_calendars", toolHelp: "The calendars a space keeps, by name, with whether the caller may change them.", tag: "calendars",
		summary:   "The space's calendars by name, and whether the caller may change each one.",
		responses: ok(env{"calendars": []calendar.Calendar{}})},
	{method: "POST", path: "/spaces/{spaceKey}/calendars", handler: "handleCreateCalendar", tool: "create_calendar", toolHelp: "Add a calendar to a space, named as no other of its calendars is.", tag: "calendars",
		summary: "Add a calendar to the space, named as none of its others is, whatever the case; at most 20. For whoever may add pages to the space, out of the archive.",
		request: calendar.CalendarInput{}, responses: map[int]any{201: env{"calendar": calendar.Calendar{}}, 409: errorEnvelope{}, 422: errorEnvelope{}}},
	{method: "PATCH", path: "/calendars/{calendarID}", handler: "handleRenameCalendar", tool: "rename_calendar", toolHelp: "Give a calendar another name.", tag: "calendars",
		summary: "Rename a calendar. For whoever may add pages to its space, out of the archive.",
		request: calendar.CalendarInput{}, responses: map[int]any{200: env{"calendar": calendar.Calendar{}}, 422: errorEnvelope{}}},
	{method: "DELETE", path: "/calendars/{calendarID}", handler: "handleDeleteCalendar", tag: "calendars",
		summary: "Remove a calendar with every event in it. For whoever may add pages to its space, out of the archive.", responses: none()},
	{method: "GET", path: "/calendars/{calendarID}/events", handler: "handleListCalendarEvents", tool: "list_calendar_events", toolHelp: "A calendar's events between two times, such as a month, by when they start.", tag: "calendars",
		summary: "A calendar and its events that fall between from and to, by when they start, the day's whole ones first; an event that lasts all day holds its last day whole. Truncated says more fell there than are answered.",
		query: []param{
			{name: "from", description: "An RFC 3339 time, the first instant asked for."},
			{name: "to", description: "An RFC 3339 time after from, at most 62 days later, the first instant not asked for."},
		}, responses: map[int]any{200: calendar.CalendarEvents{}, 422: errorEnvelope{}}},
	{method: "POST", path: "/calendars/{calendarID}/events", handler: "handleCreateCalendarEvent", tool: "create_calendar_event", toolHelp: "Add an event or an absence to a calendar, over whole days or between two times.", tag: "calendars",
		summary: "Add an event or an absence to a calendar. One that lasts all day starts and ends at midnight UTC, its end the last day it covers; at most 366 days. For whoever may add pages to its space, out of the archive.",
		request: calendar.CalendarEventInput{}, responses: map[int]any{201: env{"event": calendar.CalendarEvent{}}, 422: errorEnvelope{}}},
	{method: "PUT", path: "/calendars/{calendarID}/events/{eventID}", handler: "handleUpdateCalendarEvent", tool: "update_calendar_event", toolHelp: "Change all of a calendar's event: its title, kind and days or times.", tag: "calendars",
		summary: "Change all of an event, as a new one is given. For whoever may add pages to its space, out of the archive.",
		request: calendar.CalendarEventInput{}, responses: map[int]any{200: env{"event": calendar.CalendarEvent{}}, 422: errorEnvelope{}}},
	{method: "DELETE", path: "/calendars/{calendarID}/events/{eventID}", handler: "handleDeleteCalendarEvent", tag: "calendars",
		summary: "Remove an event from its calendar. For whoever may add pages to its space, out of the archive.", responses: none()},
	{method: "GET", path: "/spaces/{spaceKey}/trash", handler: "handleListTrash", tag: "trash", summary: "The space's trash, the latest first.", responses: ok(env{"items": []page.TrashItem{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}/trash", handler: "handleEmptyTrash", tag: "trash", summary: "Delete everything in the space's trash for good. For administrators.", responses: none()},
	{method: "POST", path: "/spaces/{spaceKey}/trash/{pageID}/restore", handler: "handleRestorePage", tag: "trash", summary: "Put a trashed page back where it was, or under the home page when that is gone.", responses: ok(env{"page": page.Page{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}/trash/{pageID}", handler: "handlePurgePage", tag: "trash", summary: "Delete a trashed page and what went with it for good. For administrators.", responses: none()},
	{method: "POST", path: "/pages", handler: "handleCreatePage", tool: "create_page", toolHelp: "Add a page under parentId; body is a document as get_page returns one, and publish true makes it visible to the space at once. kind folder makes a folder, which holds pages and has no body.", tag: "pages", summary: "Add a page or a folder under a parent, last unless a place is named; a page is unpublished and its creator's alone unless publish is set, a folder is seen at once.", request: page.CreateInput{}, responses: created(env{"page": page.Page{}})},
	{method: "PUT", path: "/pages/{pageID}/appearance", handler: "handleSetAppearance", tag: "pages", summary: "Replace how a page looks: one emoji before its title and in the tree, fixed or full width, and one of its own pictures as its cover with the point that stays in view. A null icon or cover takes it away. For the page's editors.", request: page.AppearanceInput{}, responses: ok(env{"appearance": page.Appearance{}})},
	{method: "GET", path: "/pages/{pageID}/included", handler: "handleGetIncluded", tag: "pages", summary: "What an include of a page shows the caller: its published body, or one excerpt's blocks. 404 for a page the caller may not read, never published, or without that excerpt; 409 for an include that leads back to a page in via or is nested too deep.",
		query: []param{
			{name: "excerpt", schema: &openapi.Schema{Type: "string", Format: "uuid"}, description: "The excerpt to show; the whole page when absent."},
			{name: "via", schema: &openapi.Schema{Type: "string"}, description: "The ids of the pages the include sits in, outermost first, separated by commas."},
		},
		responses: ok(env{"included": page.Included{}})},
	{method: "GET", path: "/pages/{pageID}/excerpts", handler: "handleListExcerpts", tool: "list_page_excerpts", toolHelp: "The named excerpts of a page's published body, in reading order, each with its id, its name and the start of its words.", tag: "pages", summary: "The named excerpts of a page's published body, in reading order, for choosing one to include elsewhere.", responses: ok(env{"excerpts": []document.Excerpt{}})},
	{method: "GET", path: "/pages/{pageID}", handler: "handleGetPage", tool: "get_page", toolHelp: "One page with its title, its body as a document, its version and its space.", tag: "pages", summary: "One page with its body, and the space it is in.", responses: ok(pageResponse{})},
	{method: "PATCH", path: "/pages/{pageID}", handler: "handleUpdatePage", tool: "update_page", toolHelp: "Publish a new title or body document as the next version; version is the one the change was made from.", tag: "pages", summary: "Publish a new title or body as the next version, with no comment, over the version it was made from; drafts are left alone.", request: page.UpdateInput{}, responses: ok(env{"page": page.Page{}})},
	{method: "DELETE", path: "/pages/{pageID}", handler: "handleTrashPage", tag: "pages", summary: "Move a page and every page below it to its space's trash.", responses: none()},
	{method: "POST", path: "/pages/{pageID}/move", handler: "handleMovePage", tag: "pages", summary: "Move a page under another, in its space or another, with or without its children; a move under itself is refused.", request: page.MoveInput{}, responses: ok(env{"page": page.Page{}})},
	{method: "POST", path: "/pages/{pageID}/copy", handler: "handleCopyPage", tag: "pages", summary: "Copy a page, with or without the pages below it, under a parent in its space or another.", request: page.CopyInput{}, responses: created(env{"page": page.Page{}})},
	{method: "GET", path: "/pages/{pageID}/below", handler: "handleListPagesBelow", tool: "list_pages_below", toolHelp: "The pages under a page the caller may view; scope subtree takes every level.", tag: "pages", summary: "The pages under a page that the caller may view, out of the trash, each after its parent, for a child pages block; truncated says the list stopped at its limit.",
		query: []param{
			{name: "scope", schema: &openapi.Schema{Type: "string", Enum: document.ChildPagesScopes}, description: "children, the default, or subtree."},
			{name: "depth", schema: intParam, description: "For subtree, how many levels down, 1 to 10; every level when absent."},
			{name: "sort", schema: &openapi.Schema{Type: "string", Enum: document.ChildPagesSorts}, description: "How siblings are ordered: tree, the default, title, or updated, the latest change first."},
		}, responses: ok(env{"pages": []page.BelowPage{}, "truncated": false})},

	// Markdown import and export (#90); docs/markdown.md says how each block is written.
	{method: "GET", path: "/pages/{pageID}/export", handler: "handleExportPage", tag: "markdown", summary: "A page as a .zip of Markdown with its files, and with subtree the pages below it the caller may view, in folders.", binary: true,
		query: []param{{name: "subtree", schema: &openapi.Schema{Type: "boolean"}, description: "true to take the pages below it too; false when absent."}}, responses: ok(nil)},
	{method: "GET", path: "/pages/{pageID}/markdown", handler: "handleGetPageMarkdown", tool: "get_page_markdown", toolHelp: "A page as Markdown, the easiest way to read it.", tag: "markdown", summary: "A page as one Markdown file, its files named where its export puts them.", binary: true,
		responses: ok(nil)},
	{method: "PUT", path: "/pages/{pageID}/markdown", handler: "handleReplacePageMarkdown", tool: "replace_page_markdown", toolHelp: "Publish Markdown in content as the next version of a page; version is the one it replaces, and a leading level 1 heading becomes the title.", toolFile: "page.md", tag: "markdown", summary: "Publish one Markdown file, sent with the files it shows as parts named file, as the next version of a page; its one leading level 1 heading becomes the title.", multipart: true,
		query:     []param{{name: "version", schema: intParam, description: "The version the Markdown replaces; a newer one refuses it with conflict."}},
		responses: map[int]any{200: env{"page": page.Page{}, "warnings": []string{}}, 409: errorEnvelope{}, 413: errorEnvelope{}}},
	{method: "POST", path: "/pages/{pageID}/import", handler: "handleImportMarkdown", tool: "import_markdown", toolHelp: "Make a new published page under pageID from Markdown in content; its leading level 1 heading becomes the title.", toolFile: "page.md", tag: "markdown", summary: "Make pages under a page from Markdown files and their folders, sent as parts named file with their paths, or as a .zip; each folder of Markdown is a page too.", multipart: true,
		responses: map[int]any{201: env{"pages": []mdio.Imported{}, "warnings": []string{}}, 413: errorEnvelope{}}},

	// Templates (#15).
	{method: "GET", path: "/templates", handler: "handleListTemplates", tool: "list_templates", toolHelp: "The documents a new page can start from.", tag: "templates", summary: "The documents a new page can start from, in the order to offer them; send one's body and title with POST /pages.",
		responses: ok(env{"templates": []template.Template{}})},
	{method: "GET", path: "/templates/{templateKey}", handler: "handleGetTemplate", tool: "get_template", toolHelp: "One template's title and body, to send to create_page.", tag: "templates", summary: "One template by its key.",
		responses: ok(env{"template": template.Template{}})},

	// Drafts and publishing (#13).
	{method: "GET", path: "/pages/{pageID}/draft", handler: "handleGetDraft", tag: "drafts", summary: "The caller's own draft of a page, or null when they have none.",
		responses: ok(env{"draft": (*page.Draft)(nil)})},
	{method: "PUT", path: "/pages/{pageID}/draft", handler: "handleSaveDraft", tag: "drafts", summary: "Autosave the caller's draft of a page; nobody else sees it.",
		request: page.DraftInput{}, responses: ok(env{"draft": page.Draft{}})},
	{method: "DELETE", path: "/pages/{pageID}/draft", handler: "handleDiscardDraft", tag: "drafts", summary: "Throw the caller's draft away; the page stays as last published.",
		responses: none()},
	{method: "POST", path: "/pages/{pageID}/publish", handler: "handlePublishPage", tag: "drafts", summary: "Publish the caller's draft as the next version; refused with publish_conflict when somebody published since the draft began.",
		request: page.PublishInput{}, responses: map[int]any{200: env{"page": page.Page{}, "version": page.VersionEntry{}}, 409: errorEnvelope{}}},

	// History (#14).
	{method: "GET", path: "/pages/{pageID}/versions", handler: "handleListVersions", tool: "list_versions", toolHelp: "A page's published versions, the latest first.", tag: "history", summary: "A page's published versions, the latest first.",
		query: pageQuery, responses: ok(env{"versions": []page.VersionEntry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/pages/{pageID}/versions/{versionNumber}", handler: "handleGetVersion", tool: "get_version", toolHelp: "One published version of a page with its body, as it was.", tag: "history", summary: "One published version with its body, to read it as it was.",
		responses: ok(env{"version": page.Version{}})},
	{method: "POST", path: "/pages/{pageID}/versions/{versionNumber}/restore", handler: "handleRestoreVersion", tag: "history", summary: "Publish an older version's title and body again, as the next version.",
		request: page.RestoreInput{}, responses: map[int]any{200: env{"page": page.Page{}, "version": page.VersionEntry{}}, 409: errorEnvelope{}}},
	{method: "GET", path: "/pages/{pageID}/compare", handler: "handleCompareVersions", tool: "compare_versions", toolHelp: "What changed between two versions of a page.", tag: "history", summary: "What changed between two versions of a page, or between a version and the caller's draft.",
		query: []param{
			{name: "from", description: "A version number, 0 for the empty page, or draft; when absent the version before to, or the draft's base version when to is draft."},
			{name: "to", description: "A version number, or draft; the latest version when absent."},
		}, responses: ok(env{"comparison": page.Comparison{}})},

	// Search (#18).
	{method: "GET", path: "/search", handler: "handleSearch", tool: "search", toolHelp: "Find pages, files and comments by their words among what the caller may see; q takes words, quoted phrases, or and -word.", tag: "search", summary: "Pages, attachments and comments whose words match, among what the caller may see.",
		query: searchQuery, responses: ok(env{"hits": []search.Hit{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/search/quick", handler: "handleQuickSearch", tag: "search", summary: "Pages whose titles start with the words typed so far, for the top bar and the command palette.",
		query: []param{
			{name: "q", description: "The words typed so far."},
			{name: "space", description: "A space key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 20; 8 when absent."},
		}, responses: ok(env{"pages": []search.PageHit{}})},
	{method: "GET", path: "/recent-pages", handler: "handleRecentPages", tag: "search", summary: "The pages the caller visited last, the latest first.",
		query: []param{{name: "limit", schema: intParam, description: "1 to 20; 10 when absent."}}, responses: ok(env{"pages": []search.RecentPage{}})},
	{method: "POST", path: "/pages/{pageID}/visit", handler: "handleVisitPage", tag: "search", summary: "Note that the caller opened a page, for their recent pages, and count the view once a day.",
		responses: none()},

	// Page views (#97).
	{method: "GET", path: "/pages/{pageID}/views", handler: "handlePageViews", tool: "get_page_views", toolHelp: "How often a page was read: views (each person once a day) and distinct readers, in all and over the last days.", tag: "views",
		summary:   "How often a page was read, each person counted once a day, and by how many people, in all and over the last days; never by whom. For anybody who may view the page.",
		responses: ok(pageview.ViewCounts{})},
	{method: "GET", path: "/pages/{pageID}/readers", handler: "handlePageReaders", tag: "views",
		summary: "Who read a page within the retention, the latest first, leaving out who chose not to be named; next is the cursor for the window after, null at the end. For people who may edit the page.",
		query:   keysetQueryOf(pageview.DefaultLimit, pageview.MaxLimit), responses: ok(pageview.Readers{})},

	// Labels (#17).
	{method: "GET", path: "/pages/{pageID}/labels", handler: "handleListPageLabels", tool: "list_page_labels", toolHelp: "The labels on a page.", tag: "labels", summary: "The labels on a page, by name.",
		responses: ok(env{"labels": []string{}})},
	{method: "POST", path: "/pages/{pageID}/labels", handler: "handleAddPageLabel", tool: "add_page_label", toolHelp: "Put a label, one lower case word, on a page.", tag: "labels", summary: "Put a label on a page, normalized to one lower case word; one it carries already is no change.",
		request: label.LabelInput{}, responses: ok(env{"labels": []string{}})},
	{method: "DELETE", path: "/pages/{pageID}/labels/{labelName}", handler: "handleRemovePageLabel", tag: "labels", summary: "Take a label off a page; one it does not carry is no change.",
		responses: none()},
	{method: "GET", path: "/labels", handler: "handleSuggestLabels", tool: "list_labels", toolHelp: "Labels in use that start with q, the most used first.", tag: "labels", summary: "Labels on pages the caller may view that start with the words typed, the most used first.",
		query: []param{
			{name: "q", description: "What was typed so far; empty offers the most used labels."},
			{name: "space", description: "A space key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 50; 10 when absent."},
		}, responses: ok(env{"labels": []label.LabelSuggestion{}})},
	{method: "GET", path: "/labels/{labelName}/pages", handler: "handleListLabelPages", tool: "list_label_pages", toolHelp: "The pages that carry a label.", tag: "labels", summary: "The pages out of the trash that carry a label and that the caller may view, by title.",
		query:     append([]param{{name: "space", description: "A space key to stay inside; a space the caller may not view is not found."}}, pageQuery...),
		responses: ok(env{"pages": []label.LabeledPage{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/properties-report", handler: "handlePropertiesReport", tool: "properties_report", toolHelp: "A register of the pages that carry every label given, with the value of each property their properties blocks set; column names the properties to show, all of them when absent.", tag: "labels", summary: "The properties of the published pages that carry every label given and that the caller may read, by title, for a properties report.",
		query: []param{
			{name: "label", repeated: true, description: "1 to 5 labels; a page carries all of them."},
			{name: "space", description: "A space key to stay inside; a space the caller may not view is not found."},
			{name: "column", repeated: true, description: "Up to 10 property names to show, in order; every name found when absent."},
		}, responses: map[int]any{200: label.PropertiesReport{}, 422: errorEnvelope{}}},
	{method: "GET", path: "/labelled-pages", handler: "handleLabelledPages", tool: "list_labelled_pages", toolHelp: "The published pages that carry all, or with match any, any of the labels given, latest first or by title.", tag: "labels", summary: "The published pages out of the trash and the archive that carry the labels, all or any, and that the caller may read, for a content by label block.",
		query: []param{
			{name: "label", repeated: true, description: "1 to 5 labels."},
			{name: "match", schema: &openapi.Schema{Type: "string", Enum: document.ListMatches}, description: "all when absent: a page carries every label; any: at least one."},
			{name: "space", description: "A space key to stay inside; a space the caller may not view is not found."},
			{name: "sort", schema: &openapi.Schema{Type: "string", Enum: document.ListSorts}, description: "updated, the latest published first, when absent; or title."},
			{name: "limit", schema: intParam, description: "1 to 50; 10 when absent."},
		}, responses: map[int]any{200: env{"pages": []label.LabeledPage{}}, 422: errorEnvelope{}}},
	{method: "GET", path: "/updated-pages", handler: "handleUpdatedPages", tool: "list_updated_pages", toolHelp: "The pages published last, by anybody, in one space or across the organization, with who published each.", tag: "pages", summary: "The pages published last that the caller may read, in a space or across the organization, folders, the trash and the archive left out, for a recently updated block.",
		query: []param{
			{name: "space", description: "A space key to stay inside; a space the caller may not view is not found."},
			{name: "limit", schema: intParam, description: "1 to 50; 10 when absent."},
		}, responses: map[int]any{200: env{"pages": []page.UpdatedPage{}}, 422: errorEnvelope{}}},

	// Permissions (#19).
	{method: "GET", path: "/access/me", handler: "handleMyAccess", tag: "permissions", summary: "What the caller may do across the organization, which decides which buttons to draw.",
		responses: ok(env{"can": perm.GlobalCan{}})},
	{method: "GET", path: "/org/permissions", handler: "handleListGlobalPermissions", orgWide: true, tag: "permissions", summary: "Each global permission and whom it is granted to. For administrators.",
		responses: ok(env{"permissions": []perm.GlobalGrant{}})},
	{method: "PUT", path: "/org/permissions/{permission}", handler: "handleSetGlobalPermission", orgWide: true, tag: "permissions", summary: "Replace whom a global permission is granted to. For administrators.",
		request: perm.GlobalGrantInput{}, responses: ok(env{"permission": perm.GlobalGrant{}})},
	{method: "GET", path: "/link-preview", handler: "handleLinkPreview", tag: "pages", summary: "What a web page says about itself, its title, summary and site, for a link's card, and the player it embeds in when its site is allowlisted. Read through the outbound guard and kept an hour.",
		query:     []param{{name: "url", schema: &openapi.Schema{Type: "string", Format: "uri"}, description: "The full address of the web page, http or https."}},
		responses: ok(env{"preview": unfurl.LinkPreview{}})},
	{method: "GET", path: "/org/hub", handler: "handleGetHub", tool: "get_hub", toolHelp: "The organization's hub page, if there is one the caller may read, and whether everybody lands on it.", tag: "hub", summary: "The organization's hub page as the caller may see it, and whether everybody lands on it.", responses: ok(env{"hub": hub.Hub{}})},
	{method: "PUT", path: "/org/hub", handler: "handleSetHub", orgWide: true, tag: "hub", summary: "Choose the organization's hub page, or none, and whether everybody lands on it. For administrators.", request: hub.HubInput{}, responses: ok(env{"hub": hub.Hub{}})},
	{method: "GET", path: "/spaces/{spaceKey}/permissions", handler: "handleListSpacePermissions", tag: "permissions", summary: "Who may do what in a space. For the space's administrators.",
		responses: ok(env{"grants": []perm.SpaceGrant{}})},
	{method: "PUT", path: "/spaces/{spaceKey}/permissions", handler: "handleSetSpacePermissions", tag: "permissions", summary: "Replace a space's whole permission table. For the space's administrators.",
		request: perm.SpaceGrantsInput{}, responses: ok(env{"grants": []perm.SpaceGrant{}})},
	{method: "GET", path: "/pages/{pageID}/restrictions", handler: "handleGetPageRestrictions", tag: "permissions", summary: "Who may view and edit a page beyond the space's permissions, and the restricted pages above it.",
		responses: ok(env{"restrictions": page.Restrictions{}})},
	{method: "PUT", path: "/pages/{pageID}/restrictions", handler: "handleSetPageRestrictions", tag: "permissions", summary: "Replace a page's own view and edit restrictions; the pages below it inherit them.",
		request: page.RestrictionsInput{}, responses: ok(env{"restrictions": page.Restrictions{}})},
	{method: "GET", path: "/pages/{pageID}/access/{userID}", handler: "handleInspectPageAccess", tag: "permissions", summary: "What a person may do to a page and which grant or restriction decides each right, as the database answers it. For the space's administrators.",
		responses: ok(env{"access": perm.AccessReport{}})},
	{method: "GET", path: "/people", handler: "handleListPeople", tool: "list_people", toolHelp: "Members of the organization by name, with the ids other tools take.", tag: "permissions", summary: "Members of the organization, to pick whom to grant something.",
		query: pickerQuery, responses: ok(env{"people": []perm.Person{}})},
	{method: "GET", path: "/groups", handler: "handleListGroups", tag: "permissions", summary: "Groups of the organization, to pick whom to grant something.",
		query: pickerQuery, responses: ok(env{"groups": []perm.Group{}})},

	// Attachments (#20), as Armature serves them.
	{method: "GET", path: "/pages/{pageID}/attachments", handler: "handleListAttachments", tool: "list_attachments", toolHelp: "The files on a page, with their names, sizes, uploaders and versions; current=true for the latest version of each name.", tag: "attachments", summary: "The files on a page, the latest first. A file uploaded under a name the page already has, whatever its case, is that name's next version.",
		query:     []param{{name: "current", schema: &openapi.Schema{Type: "boolean"}, description: "true lists only the latest version of each name; false when absent."}},
		responses: ok(env{"attachments": []attachment.Attachment{}})},
	{method: "POST", path: "/pages/{pageID}/attachments", handler: "handleUploadAttachment", tag: "attachments", summary: "Put a file on a page, as a multipart part named file; refused with too_large over the upload limit.", multipart: true,
		responses: map[int]any{201: env{"attachment": attachment.Attachment{}}, 413: errorEnvelope{}}},
	{method: "GET", path: "/attachments/{attachmentID}", handler: "handleDownloadAttachment", tag: "attachments", summary: "The bytes of a file, as a download.", binary: true,
		query: []param{{name: "inline", description: "1 to show images, PDFs and text in place."}}, responses: ok(nil)},
	{method: "GET", path: "/attachments/{attachmentID}/preview", handler: "handlePreviewAttachment", tag: "attachments", summary: "A file as a PDF to show in place: a PDF itself, or an office document converted once and kept. Refused with no_preview for any other file, and with preview_failed, preview_too_large, preview_off or preview_unavailable when there is no PDF to show.", binary: true,
		responses: map[int]any{200: nil, 413: errorEnvelope{}, 415: errorEnvelope{}, 422: errorEnvelope{}, 503: errorEnvelope{}}},
	{method: "DELETE", path: "/attachments/{attachmentID}", handler: "handleDeleteAttachment", tag: "attachments", summary: "Take a file off its page for good.",
		responses: none()},

	// Comments (#22) and inline comments (#23); see docs/api-contract-m2.md.
	{method: "GET", path: "/pages/{pageID}/comments", handler: "handleListComments", tool: "list_comments", toolHelp: "A page's comment threads, oldest first.", tag: "comments",
		summary:   "A page's threads with their comments, oldest first: below the page, inline, or both; resolved ones included.",
		query:     []param{{name: "kind", schema: &openapi.Schema{Type: "string", Enum: enumStrings(comment.Kinds)}, description: "page or inline; both when absent."}},
		responses: ok(env{"threads": []comment.Thread{}})},
	{method: "POST", path: "/pages/{pageID}/comments", handler: "handleStartThread", tool: "add_comment", toolHelp: "Start a comment thread below a published page; body is a document like a page's.", tag: "comments",
		summary: "Start a thread below a published page; refused with unpublished before its first publish.",
		request: comment.ThreadInput{}, responses: map[int]any{201: env{"thread": comment.Thread{}}, 409: errorEnvelope{}}},
	{method: "POST", path: "/pages/{pageID}/inline-comments", handler: "handleStartInlineThread", tag: "comments",
		summary: "Start a thread on a passage, sending the published body with the passage marked; refused with anchor_conflict when the body changed meanwhile.",
		request: comment.InlineThreadInput{}, responses: map[int]any{201: env{"thread": comment.Thread{}, "page": page.Page{}}, 409: errorEnvelope{}}},
	{method: "GET", path: "/comments/{commentID}", handler: "handleGetThread", tool: "get_comment_thread", toolHelp: "The whole thread a comment belongs to.", tag: "comments",
		summary:   "The whole thread a comment belongs to, where a notification leads.",
		responses: ok(env{"thread": comment.Thread{}})},
	{method: "PATCH", path: "/comments/{commentID}", handler: "handleEditComment", tag: "comments",
		summary: "Rewrite one's own comment; nobody else may.",
		request: comment.BodyInput{}, responses: ok(env{"comment": comment.Comment{}})},
	{method: "DELETE", path: "/comments/{commentID}", handler: "handleDeleteComment", tag: "comments",
		summary:   "Delete one's own comment, or anybody's with the space's delete permission; its words go, its replies stay.",
		responses: none()},
	{method: "POST", path: "/comments/{commentID}/replies", handler: "handleReply", tool: "reply_to_comment", toolHelp: "Reply at the end of the thread a comment belongs to; body is a document like a page's.", tag: "comments",
		summary: "Reply at the end of the thread a comment belongs to; a resolved thread opens again.",
		request: comment.BodyInput{}, responses: created(env{"comment": comment.Comment{}, "thread": comment.Thread{}})},
	{method: "POST", path: "/comments/{commentID}/resolve", handler: "handleResolveThread", tag: "comments",
		summary:   "Mark the inline thread a comment belongs to resolved; refused with not_inline below the page.",
		responses: map[int]any{200: env{"thread": comment.Thread{}}, 409: errorEnvelope{}}},
	{method: "POST", path: "/comments/{commentID}/reopen", handler: "handleReopenThread", tag: "comments",
		summary:   "Open a resolved inline thread again; refused with not_inline below the page.",
		responses: map[int]any{200: env{"thread": comment.Thread{}}, 409: errorEnvelope{}}},

	// Reactions (#66).
	{method: "POST", path: "/pages/{pageID}/reactions", handler: "handleReactToPage", tag: "reactions",
		summary: "Put the caller's emoji on a page; again is no change. Needs the right to comment; refused with unpublished before the first publish.",
		request: reaction.Input{}, responses: map[int]any{200: env{"reactions": []reaction.Reaction{}}, 409: errorEnvelope{}}},
	{method: "DELETE", path: "/pages/{pageID}/reactions", handler: "handleUnreactPage", tag: "reactions",
		summary: "Take the caller's emoji off a page, if it is there.",
		query:   emojiQuery, responses: ok(env{"reactions": []reaction.Reaction{}})},
	{method: "POST", path: "/comments/{commentID}/reactions", handler: "handleReactToComment", tag: "reactions",
		summary: "Put the caller's emoji on a comment that is not deleted; again is no change. Needs the right to comment.",
		request: reaction.Input{}, responses: ok(env{"reactions": []reaction.Reaction{}})},
	{method: "DELETE", path: "/comments/{commentID}/reactions", handler: "handleUnreactComment", tag: "reactions",
		summary: "Take the caller's emoji off a comment, if it is there.",
		query:   emojiQuery, responses: ok(env{"reactions": []reaction.Reaction{}})},

	// Owners and verification (#68).
	{method: "PUT", path: "/pages/{pageID}/owner", handler: "handleSetPageOwner", tag: "verification",
		summary: "Name the person who answers for a page, a member who may view it; needs edit of the published page.",
		request: page.OwnerInput{}, responses: map[int]any{200: env{"owner": page.Owner{}}, 422: errorEnvelope{}}},
	{method: "DELETE", path: "/pages/{pageID}/owner", handler: "handleRemovePageOwner", tag: "verification",
		summary:   "Leave a page without an owner, also when it had none; needs edit.",
		responses: none()},
	{method: "PUT", path: "/pages/{pageID}/verification", handler: "handleVerifyPage", tag: "verification",
		summary: "Say the page is right as it stands, for days days (90 when left out, at most 730); replaces any verification before. The owner is told when it runs out.",
		request: page.VerifyInput{}, responses: map[int]any{200: env{"verification": page.Verification{}}, 422: errorEnvelope{}}},
	{method: "DELETE", path: "/pages/{pageID}/verification", handler: "handleUnverifyPage", tag: "verification",
		summary:   "Take a page's verification away, also when it had none; needs edit.",
		responses: none()},

	// Mentions (#24).
	{method: "GET", path: "/pages/{pageID}/mentionable", handler: "handleListMentionable", tag: "mentions",
		summary: "Members to mention on a page, each saying whether they may view it once published; only those are told.",
		query:   pickerQuery, responses: ok(env{"people": []perm.Mentionable{}})},

	// Sharing (#67).
	{method: "POST", path: "/pages/{pageID}/share", handler: "handleSharePage", tag: "sharing",
		summary: "Send a published page, with an optional note, to people and groups who may view it; refused with cannot_view, sending nothing, when it is closed to any of them, and with rate_limited past the hourly limit.",
		request: share.Input{}, responses: map[int]any{201: env{"share": share.Share{}}, 409: errorEnvelope{}, 422: errorEnvelope{}, 429: errorEnvelope{}}},
	{method: "GET", path: "/pages/{pageID}/share/recipients", handler: "handleShareRecipients", tag: "sharing",
		summary: "Members and groups to share a page with, each saying whether, or how many of its members, may view it; only those are told.",
		query:   pickerQuery, responses: ok(env{"people": []share.Recipient{}, "groups": []share.RecipientGroup{}})},
	{method: "GET", path: "/pages/{pageID}/viewers", handler: "handleListViewers", tag: "sharing",
		summary: "Who may view a page, by name, and whether that is every member of the organization.",
		query:   pageQuery, responses: ok(env{"viewers": []perm.Person{}, "total": 0, "everyone": false, "limit": 0, "offset": 0})},

	// Watching (#25).
	{method: "PUT", path: "/pages/{pageID}/watch", handler: "handleWatchPage", tag: "watching",
		summary: "Watch a page alone, or with every page below it; replaces the caller's own watch on it.",
		request: watch.Input{}, responses: ok(env{"watching": watch.Watching{}})},
	{method: "DELETE", path: "/pages/{pageID}/watch", handler: "handleUnwatchPage", tag: "watching",
		summary:   "Stop watching a page; a watch above it still covers it, and the caller's edits no longer watch it again.",
		responses: none()},
	{method: "GET", path: "/pages/{pageID}/watchers", handler: "handleListWatchers", tag: "watching",
		summary: "Who hears about a page, by name, and through which watch; only people who may view it.",
		query:   pageQuery, responses: ok(env{"watchers": []watch.Watcher{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "PUT", path: "/spaces/{spaceKey}/watch", handler: "handleWatchSpace", tag: "watching",
		summary:   "Watch every page of a space, now and later.",
		responses: none()},
	{method: "DELETE", path: "/spaces/{spaceKey}/watch", handler: "handleUnwatchSpace", tag: "watching",
		summary:   "Stop watching a space; watches on its pages stay.",
		responses: none()},
	{method: "GET", path: "/watches", handler: "handleListWatches", tag: "watching",
		summary: "The caller's own watches on what they may still view, the latest first.",
		query:   pageQuery, responses: ok(env{"watches": []watch.Watch{}, "total": 0, "limit": 0, "offset": 0})},

	// Stars and the home page (#38).
	{method: "PUT", path: "/pages/{pageID}/star", handler: "handleStarPage", tag: "home",
		summary:   "Star a page to keep it on the caller's home page; starring it again is no change.",
		responses: none()},
	{method: "DELETE", path: "/pages/{pageID}/star", handler: "handleUnstarPage", tag: "home",
		summary:   "Take the caller's star off a page, also when there was none.",
		responses: none()},
	{method: "PUT", path: "/spaces/{spaceKey}/star", handler: "handleStarSpace", tag: "home",
		summary:   "Star a space to keep it on the caller's home page.",
		responses: none()},
	{method: "DELETE", path: "/spaces/{spaceKey}/star", handler: "handleUnstarSpace", tag: "home",
		summary:   "Take the caller's star off a space; the stars on its pages stay.",
		responses: none()},
	{method: "GET", path: "/stars", handler: "handleListStars", tag: "home",
		summary: "The caller's stars on what they may still view, the latest first; next is the cursor for the window after, null at the end.",
		query:   keysetQuery(100), responses: ok(env{"stars": []star.Star{}, "next": (*string)(nil)})},
	{method: "GET", path: "/home/updates", handler: "handleHomeUpdates", tool: "list_recent_updates", toolHelp: "Pages others published lately that the caller may view, the latest first.", tag: "home",
		summary:   "Pages others published that the caller may view, each once, the latest first; with scope watched only those the caller's watches cover.",
		query:     append([]param{{name: "scope", schema: &openapi.Schema{Type: "string", Enum: enumStrings(home.Scopes)}, description: "all when absent."}}, keysetQuery(50)...),
		responses: ok(env{"updates": []home.PageUpdate{}, "next": (*string)(nil)})},
	{method: "GET", path: "/home/edited", handler: "handleHomeEdited", tag: "home",
		summary: "Pages the caller published, holds a draft of, or made and never published, that they may still view, the latest first.",
		query:   keysetQuery(50), responses: ok(env{"pages": []home.EditedPage{}, "next": (*string)(nil)})},

	// Tasks (#56): checklist items read from published pages.
	{method: "GET", path: "/tasks", handler: "handleListMyTasks", tool: "list_my_tasks", toolHelp: "The tasks assigned to the caller on pages they may view: open ones soonest due first, or done ones latest first.", tag: "tasks",
		summary:   "The tasks assigned to the caller on published pages they may still view, out of the trash and the archive: open ones soonest due first and those without a day last, or done ones the latest first; next is the cursor for the window after, null at the end.",
		query:     append([]param{{name: "state", schema: &openapi.Schema{Type: "string", Enum: enumStrings(task.States)}, description: "open when absent."}}, keysetQueryOf(task.DefaultLimit, task.MaxLimit)...),
		responses: ok(env{"tasks": []task.Task{}, "next": (*string)(nil)})},
	{method: "GET", path: "/task-report", handler: "handleTaskReport", tool: "task_report", toolHelp: "The tasks of pages in one space or all, picked by assignee, due day and state: open ones soonest due first, then done ones.", tag: "tasks",
		summary: "The tasks of published pages the caller may view, out of the trash and the archive, that the filter picks, for a task report block: open ones soonest due first and those without a day last, then done ones the latest first; truncated says more matched.",
		query: []param{
			{name: "space", description: "A space key to stay inside; a space the caller may not view is not found."},
			{name: "assignee", description: "me for the caller, none for tasks nobody is assigned, or a person's id; anybody when absent."},
			{name: "due", schema: &openapi.Schema{Type: "string", Enum: document.TaskReportDues}, description: "any when absent; overdue, today and week (today and the six days after) judge by today in UTC; none is tasks without a day."},
			{name: "state", schema: &openapi.Schema{Type: "string", Enum: document.TaskReportStates}, description: "open when absent."},
			{name: "limit", schema: intParam, description: "1 to 100; 20 when absent."},
		}, responses: map[int]any{200: task.Report{}, 422: errorEnvelope{}}},
	{method: "PATCH", path: "/pages/{pageID}/tasks/{taskID}", handler: "handleSetTaskDone", tool: "set_task_done", toolHelp: "Tick a task of a page off, or open it again, which publishes the page as its next version.", tag: "tasks",
		summary: "Tick a task off or open it again, by publishing the page with its box changed as the next version; its state already is no change. For whoever may edit the page.",
		request: task.SetDoneInput{}, responses: ok(env{"task": task.Task{}})},

	// The stale content report (#99).
	{method: "GET", path: "/stale-pages", handler: "handleListStalePages", tool: "list_stale_pages", toolHelp: "Published pages nobody published or opened for olderThan days, the longest untouched first, in the spaces the caller administers. For administrators.", tag: "stale",
		summary: "Published pages nobody published or opened within the period, in the spaces the caller administers, archived ones only when asked for, the longest untouched first; next is the cursor for the window after, null at the end. For administrators of a space or of the organization.",
		query:   append(staleQuery, keysetQueryOf(stale.DefaultLimit, stale.MaxLimit)...), responses: ok(env{"pages": []stale.StalePage{}, "next": (*string)(nil)})},

	// Notifications (#26), as Armature serves them.
	{method: "GET", path: "/notifications", handler: "handleListNotifications", tool: "list_notifications", toolHelp: "What the caller was told about pages, the latest first.", tag: "notifications",
		summary:   "What the caller was told about pages they may still view, the latest first.",
		query:     append([]param{{name: "unread", schema: &openapi.Schema{Type: "boolean"}, description: "true lists only what is not read yet."}}, pageQuery...),
		responses: ok(env{"notifications": []notify.Notification{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/notifications/unread-count", handler: "handleUnreadCount", tag: "notifications",
		summary:   "How many notifications are unread, for the badge; the client polls it.",
		responses: ok(env{"unread": 0})},
	{method: "POST", path: "/notifications/read", handler: "handleMarkRead", tag: "notifications",
		summary: "Mark the named notifications read, or all of them.",
		request: notify.MarkReadInput{}, responses: none()},
	{method: "GET", path: "/notification-preferences", handler: "handleNotificationPreferences", tag: "notifications",
		summary:   "How the caller wants to be told: which kinds in the app and by mail, the digest, and watching their own pages.",
		responses: ok(env{"preferences": notify.Preferences{}})},
	{method: "PUT", path: "/notification-preferences", handler: "handleSaveNotificationPreferences", tag: "notifications",
		summary: "Replace how the caller wants to be told.",
		request: notify.Preferences{}, responses: ok(env{"preferences": notify.Preferences{}})},

	// Connecting Armature (#27); see docs/api-contract-m3.md.
	{method: "GET", path: "/armature/connection", handler: "handleGetArmatureConnection", orgWide: true, tag: "armature",
		summary:   "The organization's Armature instance and what to enter in Armature's webhook settings, or null. For administrators.",
		responses: ok(env{"connection": (*armature.Connection)(nil)})},
	{method: "PUT", path: "/armature/connection", handler: "handleSaveArmatureConnection", orgWide: true, tag: "armature",
		summary: "Connect an Armature instance; a new address or organization forgets every stored token. For administrators.",
		request: armature.ConnectionInput{}, responses: ok(env{"connection": armature.Connection{}})},
	{method: "DELETE", path: "/armature/connection", handler: "handleRemoveArmatureConnection", orgWide: true, tag: "armature",
		summary:   "Disconnect Armature, forgetting every stored token and the webhook secret. For administrators.",
		responses: none()},

	// The audit log (#107), as Armature's administrators read theirs.
	{method: "GET", path: "/audit", handler: "handleListAudit", orgWide: true, tool: "list_audit_log", toolHelp: "Who did what in the organization, newest first. For administrators.", tag: "audit",
		summary: "Who did what to the organization, newest first: members, sign-in, tokens, spaces, permissions, deletions for good and exports; next is the cursor for the window after, null at the end. For administrators.",
		query:   append(auditQuery, keysetQueryOf(audit.DefaultLimit, audit.MaxLimit)...), responses: ok(env{"entries": []audit.AuditEntry{}, "next": (*string)(nil)})},
	{method: "GET", path: "/audit/facets", handler: "handleAuditFacets", orgWide: true, tag: "audit",
		summary:   "The actions, people and kinds of target the log holds, to narrow it by, and how many days an entry is kept, 0 for ever. For administrators.",
		responses: ok(env{"facets": audit.AuditFacets{}})},
	{method: "GET", path: "/audit/export", handler: "handleExportAudit", orgWide: true, tag: "audit",
		summary: "The log as a CSV file, newest first, narrowed like the list; the export is itself recorded. Refused with export_too_large past " + strconv.Itoa(audit.MaxExport) + " entries. For administrators.",
		binary:  true, query: auditQuery, responses: map[int]any{200: nil, 422: errorEnvelope{}}},

	// Webhooks (#110), as Armature's administrators keep theirs.
	{method: "GET", path: "/webhooks", handler: "handleListWebhooks", orgWide: true, tag: "webhooks",
		summary:   "Where the organization's events are posted, by name, without their secrets. For administrators.",
		responses: ok(env{"webhooks": []webhook.Webhook{}})},
	{method: "POST", path: "/webhooks", handler: "handleCreateWebhook", orgWide: true, tag: "webhooks",
		summary: "Add a webhook; its secret is in this answer and never again. The caller becomes its owner, whose permissions every payload is read with. For administrators.",
		request: webhook.WebhookInput{}, responses: map[int]any{201: env{"webhook": webhook.Webhook{}}, 422: errorEnvelope{}}},
	{method: "PATCH", path: "/webhooks/{webhookID}", handler: "handleUpdateWebhook", orgWide: true, tag: "webhooks",
		summary: "Change a webhook's name, address, topics or whether it is on; the caller becomes its owner, and turning it on clears its failures. For administrators.",
		request: webhook.WebhookInput{}, responses: map[int]any{200: env{"webhook": webhook.Webhook{}}, 422: errorEnvelope{}}},
	{method: "DELETE", path: "/webhooks/{webhookID}", handler: "handleDeleteWebhook", orgWide: true, tag: "webhooks",
		summary:   "Remove a webhook and its log. For administrators.",
		responses: none()},
	{method: "POST", path: "/webhooks/{webhookID}/rotate-secret", handler: "handleRotateWebhookSecret", orgWide: true, tag: "webhooks",
		summary:   "Issue a new secret, in this answer and never again; the old one stops at once. For administrators.",
		responses: ok(env{"webhook": webhook.Webhook{}})},
	{method: "POST", path: "/webhooks/{webhookID}/test", handler: "handleTestWebhook", orgWide: true, tag: "webhooks",
		summary:   "Post a ping now, even while the webhook is off, and answer the attempt as logged. For administrators.",
		responses: ok(env{"delivery": webhook.WebhookDelivery{}})},
	{method: "GET", path: "/webhooks/{webhookID}/deliveries", handler: "handleListWebhookDeliveries", orgWide: true, tag: "webhooks",
		summary:   "A webhook's log, one row per attempt, newest first; kept for " + strconv.Itoa(int(webhook.DeliveryRetention.Hours()/24)) + " days. For administrators.",
		query:     []param{{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(webhook.MaxDeliveries) + "; " + strconv.Itoa(webhook.DefaultDeliveries) + " when absent."}},
		responses: map[int]any{200: env{"deliveries": []webhook.WebhookDelivery{}}, 422: errorEnvelope{}}},
	{method: "POST", path: "/webhooks/{webhookID}/deliveries/{deliveryID}/redeliver", handler: "handleRedeliverWebhook", orgWide: true, tag: "webhooks",
		summary:   "Send a logged delivery's event again, now, as its next attempt, with the payload read afresh as the owner. For administrators.",
		responses: ok(env{"delivery": webhook.WebhookDelivery{}})},

	{method: "GET", path: "/armature/account", handler: "handleGetArmatureAccount", tag: "armature",
		summary:   "Whether the caller connected their Armature token, and whom it acts as.",
		responses: ok(env{"account": armature.Account{}})},
	{method: "PUT", path: "/armature/account/token", handler: "handleConnectArmatureAccount", tag: "armature",
		summary: "Store the caller's Armature personal access token, once Armature accepts it; it is never answered again.",
		request: armature.TokenInput{}, responses: ok(env{"account": armature.Account{}})},
	{method: "POST", path: "/armature/account/check", handler: "handleCheckArmatureAccount", tag: "armature",
		summary:   "Ask Armature now whether the caller's stored token still works.",
		responses: ok(env{"account": armature.Account{}})},
	{method: "DELETE", path: "/armature/account/token", handler: "handleDisconnectArmatureAccount", tag: "armature",
		summary:   "Forget the caller's Armature token.",
		responses: none()},

	// Issues in pages (#28, #29, #30, #31), each call made as the caller.
	{method: "GET", path: "/armature/issues", handler: "handleLookupArmatureIssues", tag: "armature",
		summary:   "Issues by key for smart links, as the caller may see them in Armature; status says why there are none.",
		query:     []param{{name: "key", repeated: true, schema: &openapi.Schema{Type: "string"}, description: "Issue keys, 1 to 50."}},
		responses: map[int]any{200: env{"status": armature.Status(""), "issues": []armature.IssueResult{}}, 422: errorEnvelope{}}},
	{method: "GET", path: "/armature/issues/{issueKey}", handler: "handleGetArmatureIssue", tag: "armature",
		summary:   "One issue for an issue block or a hover card; null when the caller may not see it.",
		responses: map[int]any{200: env{"status": armature.Status(""), "issue": (*armature.Issue)(nil)}, 422: errorEnvelope{}}},
	{method: "GET", path: "/armature/search", handler: "handleSearchArmatureIssues", tag: "armature",
		summary: "Issues an NQL query matches, for an issue list block; refused with bad_query and its position.",
		query: []param{
			{name: "q", description: "An NQL query, at most 2000 characters."},
			{name: "limit", schema: intParam, description: "1 to 100; 20 when absent."},
			{name: "offset", schema: intParam, description: "How many matches to skip; 0 when absent."},
		}, responses: map[int]any{200: env{"status": armature.Status(""), "issues": []armature.Issue{}, "total": 0, "limit": 0, "offset": 0, "url": ""}, 422: errorEnvelope{}}},
	{method: "GET", path: "/armature/chart", handler: "handleArmatureChart", tag: "armature",
		summary: "A count of the issues an NQL query matches in one project, as the caller may see them, for a chart block: shared out by a field for a pie, or created and resolved each day. Refused with bad_query and its position.",
		query: []param{
			{name: "project", description: "The project's key, such as CP."},
			{name: "q", description: "An NQL query, at most 2000 characters."},
			{name: "kind", schema: &openapi.Schema{Type: "string", Enum: enumStrings(armature.ChartKinds)}, description: "pie or createdResolved."},
			{name: "groupBy", schema: &openapi.Schema{Type: "string", Enum: armature.ChartGroupings}, description: "The field a pie shares the issues out by."},
			{name: "days", schema: intParam, description: "How many days back created against resolved counts, 7 to 365; 30 when absent."},
		}, responses: map[int]any{200: env{"status": armature.Status(""), "chart": (*armature.Chart)(nil)}, 422: errorEnvelope{}}},
	{method: "GET", path: "/armature/roadmap", handler: "handleArmatureRoadmap", tag: "armature",
		summary: "The issues an NQL query matches in one project, as the caller may see them, on a timeline of their start and due days for a roadmap block: under their epics or their teams. Refused with bad_query and its position.",
		query: []param{
			{name: "project", description: "The project's key, such as CP."},
			{name: "q", description: "An NQL query, at most 2000 characters."},
			{name: "groupBy", schema: &openapi.Schema{Type: "string", Enum: enumStrings(armature.RoadmapGroupings)}, description: "epic or team."},
		}, responses: map[int]any{200: env{"status": armature.Status(""), "roadmap": (*armature.Roadmap)(nil)}, 422: errorEnvelope{}}},
	{method: "GET", path: "/armature/calendar", handler: "handleArmatureCalendar", tag: "armature",
		summary: "The issues Armature dates in one month of one project, as the caller may see them, for a calendar block beside its events: each with its first and its due day.",
		query: []param{
			{name: "project", description: "The project's key, such as CP."},
			{name: "month", description: "The month as YYYY-MM."},
		}, responses: map[int]any{200: env{"status": armature.Status(""), "month": (*armature.CalendarMonth)(nil)}, 422: errorEnvelope{}}},
	{method: "GET", path: "/armature/projects", handler: "handleListArmatureProjects", tag: "armature",
		summary:   "The Armature projects the caller may see, and whether they may file issues in each.",
		responses: ok(env{"status": armature.Status(""), "projects": []armature.Project{}})},
	{method: "GET", path: "/armature/issue-types", handler: "handleListArmatureIssueTypes", tag: "armature",
		summary:   "The issue types a new issue may take, subtasks left out.",
		responses: ok(env{"status": armature.Status(""), "issueTypes": []armature.IssueType{}})},
	{method: "POST", path: "/armature/issues", handler: "handleCreateArmatureIssues", tag: "armature",
		summary: "File one Armature issue per item of a selection, in order, stopping at the first Armature refuses.",
		request: armature.CreateIssuesInput{}, responses: map[int]any{201: env{"issues": []armature.Issue{}, "failed": (*armature.CreateFailure)(nil)}, 409: errorEnvelope{}, 502: errorEnvelope{}}},

	// Pages in Armature (#32).
	{method: "GET", path: "/pages/{pageID}/armature-links", handler: "handleListArmatureLinks", tag: "armature",
		summary:   "The issues a page's published version names, and whether each carries its remote link in Armature yet.",
		responses: ok(env{"links": []armature.Link{}})},

	// Webhooks (#33): Armature signs, so nobody signs in.
	{method: "POST", path: "/armature/webhook/{orgSlug}", handler: "handleArmatureWebhook", tag: "armature", public: true,
		summary: "Where Armature posts issue events, signed with the organization's webhook secret; clears the cached issues they name.",
		request: armature.WebhookEnvelope{}, responses: map[int]any{204: nil, 401: errorEnvelope{}, 413: errorEnvelope{}}},

	// Following the Armature theme (#34).
	{method: "GET", path: "/armature/theme", handler: "handleArmatureThemeFollow", tag: "armature",
		summary:   "Whether the caller follows their active Armature theme, and whether Armature answered.",
		responses: ok(env{"follow": armature.ThemeFollow{}})},
	{method: "PUT", path: "/armature/theme", handler: "handleFollowArmatureTheme", tag: "armature",
		summary:   "Follow the caller's active Armature theme instead of a Stator one; GET /themes/active then answers it.",
		responses: map[int]any{200: env{"follow": armature.ThemeFollow{}}, 409: errorEnvelope{}, 422: errorEnvelope{}}},
	{method: "DELETE", path: "/armature/theme", handler: "handleUnfollowArmatureTheme", tag: "armature",
		summary:   "Stop following the Armature theme and return to the organization's default.",
		responses: none()},

	{method: "POST", path: "/mcp", handler: "handleMCP", id: "mcp", tag: "mcp", summary: "The Model Context Protocol endpoint: the marked operations of this API as tools, run as the caller.",
		raw: true, rawNote: "A JSON-RPC 2.0 request as the Model Context Protocol defines it.", responses: map[int]any{200: rpcResponse{}, 202: nil}},
}

var (
	intParam  = &openapi.Schema{Type: "integer"}
	pageQuery = []param{
		{name: "limit", schema: intParam, description: "1 to 100; 20 when absent."},
		{name: "offset", schema: intParam},
	}
	emojiQuery  = []param{{name: "emoji", description: "The emoji to take off, as it was put on."}}
	pickerQuery = []param{
		{name: "q", description: "Words the name, or a person's email, starts with."},
		{name: "limit", schema: intParam, description: "1 to 50; 20 when absent."},
	}
	searchQuery = []param{
		{name: "q", description: "Words to find: each must match, \"quoted words\" match as a phrase, or matches either side, -word leaves out what has it. Empty lists by the filters alone."},
		{name: "space", repeated: true, description: "Space keys to stay inside."},
		{name: "author", repeated: true, schema: &openapi.Schema{Type: "string", Format: "uuid"}, description: "People who published a version of the page, uploaded the file or wrote the comment."},
		{name: "label", repeated: true, description: "Label names; a hit carries at least one."},
		{name: "type", repeated: true, schema: &openapi.Schema{Type: "string", Enum: enumStrings(search.HitTypes)}, description: "What to find; everything when absent."},
		{name: "updatedAfter", schema: &openapi.Schema{Type: "string", Format: "date"}, description: "Changed on or after this day."},
		{name: "updatedBefore", schema: &openapi.Schema{Type: "string", Format: "date"}, description: "Changed before this day."},
		{name: "sort", schema: &openapi.Schema{Type: "string", Enum: search.Sorts}, description: "relevance unless said, and updated when q is empty."},
		{name: "limit", schema: intParam, description: "1 to 100; 20 when absent."},
		{name: "offset", schema: intParam},
	}
)

// keysetQuery is a window of a list read newest first: how many, and where
// the window before it ended.
func keysetQuery(max int) []param {
	return keysetQueryOf(20, max)
}

// keysetQueryOf is keysetQuery for a list whose window is not 20 by default.
func keysetQueryOf(def, max int) []param {
	return []param{
		{name: "limit", schema: intParam, description: "1 to " + strconv.Itoa(max) + "; " + strconv.Itoa(def) + " when absent."},
		{name: "cursor", description: "The next of the window before; the first window when absent."},
	}
}

// staleQuery narrows the stale content report; every part is optional.
var staleQuery = []param{
	{name: "space", description: "A space key; every space the caller administers when absent."},
	{name: "owner", description: "The id of the person who answers for the pages, or none for pages without an owner."},
	{name: "verification", schema: &openapi.Schema{Type: "string", Enum: enumStrings(stale.Verifications)}, description: "Where the pages stand on being checked; any when absent."},
	{name: "archived", schema: &openapi.Schema{Type: "boolean"}, description: "true to list archived pages too; false when absent."},
	{name: "olderThan", schema: intParam, description: "Days since a page was last published or opened, " + strconv.Itoa(stale.MinDays) + " to " + strconv.Itoa(stale.MaxDays) + "; " + strconv.Itoa(stale.DefaultDays) + " when absent."},
}

// auditQuery narrows the audit log; every part is optional.
var auditQuery = []param{
	{name: "action", schema: &openapi.Schema{Type: "string", Enum: audit.Actions}, description: "One action."},
	{name: "actor", schema: &openapi.Schema{Type: "string", Format: "uuid"}, description: "The person who acted."},
	{name: "targetType", description: "What kind of thing the entries are about, such as space or user."},
	{name: "target", schema: &openapi.Schema{Type: "string", Format: "uuid"}, description: "The id of the thing the entries are about."},
	{name: "from", description: "The first day, YYYY-MM-DD in UTC, or the first instant with its zone."},
	{name: "to", description: "The last day, YYYY-MM-DD in UTC and inclusive, or the instant the range ends before."},
}

// isErrorEnvelope marks a refusal a client has to tell apart, listed with its
// status next to the successes.
func isErrorEnvelope(body any) bool {
	_, is := body.(errorEnvelope)
	return is
}

// enumStrings spells a named string type's values for a schema.
func enumStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// specBuilder is the builder with every override the table relies on, shared
// by the document and the MCP tools so both describe a type alike.
func specBuilder() *openapi.Builder {
	b := openapi.NewBuilder()
	b.Names[reflect.TypeOf(page.CreateInput{})] = "PageCreateInput"
	b.Names[reflect.TypeOf(page.UpdateInput{})] = "PageUpdateInput"
	b.Names[reflect.TypeOf(reaction.Input{})] = "ReactionInput"
	b.Names[reflect.TypeOf(share.Input{})] = "ShareInput"
	b.Names[reflect.TypeOf(watch.Input{})] = "WatchInput"
	b.Names[reflect.TypeOf(task.PageRef{})] = "TaskPage"
	b.Names[reflect.TypeOf(task.SetDoneInput{})] = "TaskSetDoneInput"
	b.Names[reflect.TypeOf(task.Report{})] = "TaskReport"
	b.FieldOverrides["LinkEmbed.kind"] = &openapi.Schema{Type: "string", Enum: unfurl.EmbedKinds}
	b.FieldOverrides["Backdrop.fit"] = &openapi.Schema{Type: "string", Enum: theme.BackdropFits}
	scopes := &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: []string{auth.ScopeRead}}}
	b.FieldOverrides["APIToken.scopes"] = scopes
	b.FieldOverrides["OrgAPIToken.scopes"] = scopes
	b.FieldOverrides["CreateTokenRequest.scopes"] = scopes
	b.Enums[reflect.TypeOf(auth.OrgRole(""))] = enumStrings(auth.OrgRoles)
	b.Enums[reflect.TypeOf(page.Kind(""))] = enumStrings(page.Kinds)
	b.Enums[reflect.TypeOf(page.Width(""))] = enumStrings(page.Widths)
	b.Enums[reflect.TypeOf(auth.RoleSource(""))] = enumStrings(auth.RoleSources)
	b.Enums[reflect.TypeOf(auth.Locale(""))] = enumStrings(auth.Locales)
	b.Enums[reflect.TypeOf(perm.SubjectType(""))] = enumStrings(perm.SubjectTypes)
	b.Enums[reflect.TypeOf(perm.GlobalPermission(""))] = enumStrings(perm.GlobalPermissions)
	b.Enums[reflect.TypeOf(perm.SpacePermission(""))] = enumStrings(perm.SpacePermissions)
	b.Enums[reflect.TypeOf(perm.Right(""))] = enumStrings(perm.Rights)
	b.Enums[reflect.TypeOf(perm.StepKind(""))] = enumStrings(perm.StepKinds)
	b.Enums[reflect.TypeOf(perm.ListKind(""))] = enumStrings(perm.ListKinds)
	b.Enums[reflect.TypeOf(attachment.PreviewKind(""))] = enumStrings(attachment.PreviewKinds)
	b.Enums[reflect.TypeOf(page.DiffChange(""))] = enumStrings(page.DiffChanges)
	b.Enums[reflect.TypeOf(page.VerificationStatus(""))] = enumStrings(page.VerificationStatuses)
	b.Enums[reflect.TypeOf(stale.Verification(""))] = enumStrings(stale.Verifications)
	b.Enums[reflect.TypeOf(search.HitType(""))] = enumStrings(search.HitTypes)
	b.Enums[reflect.TypeOf(comment.Kind(""))] = enumStrings(comment.Kinds)
	b.Enums[reflect.TypeOf(comment.AnchorState(""))] = enumStrings(comment.AnchorStates)
	b.Enums[reflect.TypeOf(watch.Kind(""))] = enumStrings(watch.Kinds)
	b.Enums[reflect.TypeOf(star.Kind(""))] = enumStrings(star.Kinds)
	b.Enums[reflect.TypeOf(shortcut.Kind(""))] = enumStrings(shortcut.Kinds)
	b.Enums[reflect.TypeOf(notify.Kind(""))] = enumStrings(notify.Kinds)
	b.Enums[reflect.TypeOf(notify.Digest(""))] = enumStrings(notify.Digests)
	b.Enums[reflect.TypeOf(armature.Status(""))] = enumStrings(armature.Statuses)
	b.Enums[reflect.TypeOf(armature.ChartKind(""))] = enumStrings(armature.ChartKinds)
	b.Enums[reflect.TypeOf(armature.RoadmapGrouping(""))] = enumStrings(armature.RoadmapGroupings)
	b.Enums[reflect.TypeOf(calendar.Kind(""))] = enumStrings(calendar.Kinds)
	b.Enums[reflect.TypeOf(armature.LinkState(""))] = enumStrings(armature.LinkStates)
	b.FieldOverrides["Issue.priority"] = &openapi.Schema{Type: "string", Enum: armature.Priorities}
	b.FieldOverrides["IssueStatus.category"] = &openapi.Schema{Type: "string", Enum: armature.StatusCategories}
	// A group grants member or admin; owner is never the provider's to give.
	granted := &openapi.Schema{Type: "string", Enum: []string{string(auth.RoleAdmin), string(auth.RoleMember)}}
	b.FieldOverrides["AuditEntry.action"] = &openapi.Schema{Type: "string", Enum: audit.Actions}
	b.FieldOverrides["AuditFacets.actions"] = &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: audit.Actions}}
	b.Enums[reflect.TypeOf(webhook.DeliveryState(""))] = enumStrings(webhook.States)
	topics := &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: webhook.Subscribable}}
	b.FieldOverrides["Webhook.topics"] = topics
	b.FieldOverrides["WebhookInput.topics"] = topics
	b.FieldOverrides["Webhook.disabledReason"] = &openapi.Schema{OneOf: []*openapi.Schema{{Type: "string", Enum: webhook.DisabledReasons}, {Type: "null"}}}
	b.FieldOverrides["WebhookDelivery.topic"] = &openapi.Schema{Type: "string", Enum: append([]string{webhook.TopicPing}, webhook.Topics...)}
	b.FieldOverrides["GroupRole.role"] = granted
	b.FieldOverrides["SetGroupRoleRequest.role"] = granted
	return b
}

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := specBuilder()

	doc := &openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:   "Stator",
			Version: "1",
			Description: "The wiki's HTTP API. Every endpoint answers JSON; errors share one envelope. " +
				"Sign in with a session cookie or an API token sent as a bearer token.",
		},
		Servers: []openapi.Server{{URL: APIPrefix, Description: "This deployment"}},
		Paths:   map[string]openapi.PathItem{},
		Components: openapi.Components{
			SecuritySchemes: map[string]openapi.SecurityScheme{
				"session": {Type: "apiKey", In: "cookie", Name: config.DefaultSessionCookie, Description: "The cookie a sign-in sets."},
				"token": {Type: "http", Scheme: "bearer", BearerFormat: auth.APITokenPrefix + "<43 characters>",
					Description: "A personal access token, made under Tokens in the account menu; a token with the read scope is refused every write, and one limited to spaces reaches nothing outside them and nothing that concerns the whole organization."},
			},
		},
		Security: []map[string][]string{{"session": {}}, {"token": {}}},
	}

	errorSchema := b.Schema(reflect.TypeOf(errorEnvelope{}))
	tags := map[string]bool{}
	for _, op := range operations {
		item := doc.Paths[op.path]
		if item == nil {
			item = openapi.PathItem{}
			doc.Paths[op.path] = item
		}
		o := &openapi.Operation{
			OperationID: op.operationID(),
			Summary:     op.summary,
			Tags:        []string{op.tag},
			Responses:   map[string]*openapi.Response{},
		}
		tags[op.tag] = true
		for _, name := range pathParams(op.path) {
			o.Parameters = append(o.Parameters, openapi.Parameter{
				Name: name, In: "path", Required: true, Schema: pathParamSchema(name),
			})
		}
		for _, q := range op.query {
			schema := q.schema
			if schema == nil {
				schema = &openapi.Schema{Type: "string"}
			}
			if q.repeated {
				schema = &openapi.Schema{Type: "array", Items: schema}
			}
			o.Parameters = append(o.Parameters, openapi.Parameter{Name: q.name, In: "query", Description: q.description, Schema: schema})
		}
		switch {
		case op.multipart:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"multipart/form-data": {Schema: &openapi.Schema{Type: "object", Required: []string{"file"},
					Properties: map[string]*openapi.Schema{"file": {Type: "string", Format: "binary"}}}},
			}}
		case op.raw:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"application/json": {Schema: &openapi.Schema{Description: op.rawNote}},
			}}
		case op.request != nil:
			o.RequestBody = &openapi.RequestBody{Required: true, Content: map[string]openapi.MediaType{
				"application/json": {Schema: b.SchemaOf(op.request)},
			}}
		}
		for status, body := range op.responses {
			r := &openapi.Response{Description: http.StatusText(status)}
			switch {
			case isErrorEnvelope(body):
				r.Content = map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}
			case op.binary && status == http.StatusOK:
				r.Content = map[string]openapi.MediaType{"*/*": {Schema: &openapi.Schema{Type: "string", Format: "binary"}}}
			case body != nil:
				r.Content = map[string]openapi.MediaType{"application/json": {Schema: envelopeSchema(b, body)}}
			}
			o.Responses[strconv.Itoa(status)] = r
		}
		if op.redirect {
			o.Responses["302"] = &openapi.Response{Description: "Found: the browser is sent on."}
		}
		o.Responses["default"] = &openapi.Response{Description: "An error, in the one shape every endpoint uses.",
			Content: map[string]openapi.MediaType{"application/json": {Schema: errorSchema}}}
		if op.public {
			o.Security = []map[string][]string{}
		}
		item[strings.ToLower(op.method)] = o
	}
	for _, tag := range openapi.SortedKeys(tags) {
		doc.Tags = append(doc.Tags, openapi.Tag{Name: tag})
	}
	doc.Components.Schemas = b.Components()
	return doc
}

// operationID is what a client calls the operation: the handler's name unless
// the table says otherwise.
func (op operation) operationID() string {
	if op.id != "" {
		return op.id
	}
	id := strings.TrimPrefix(op.handler, "handle")
	return strings.ToLower(id[:1]) + id[1:]
}

// envelopeSchema describes a response body: an env becomes an inline object
// with every key required, anything else is the type itself.
func envelopeSchema(b *openapi.Builder, body any) *openapi.Schema {
	e, ok := body.(env)
	if !ok {
		return b.SchemaOf(body)
	}
	s := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	for _, key := range openapi.SortedKeys(e) {
		schema := b.SchemaOf(e[key])
		if schema == nil {
			schema = &openapi.Schema{Description: "A JSON value."}
		}
		s.Properties[key] = schema
		s.Required = append(s.Required, key)
	}
	return s
}

var pathParamPattern = regexp.MustCompile(`\{(\w+)\}`)

func pathParams(path string) []string {
	var out []string
	for _, m := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

// pathParamSchema types a path parameter by its name: ids are uuids, numbers
// integers, a permission one of their names, the rest strings.
func pathParamSchema(name string) *openapi.Schema {
	switch {
	case strings.HasSuffix(name, "ID"):
		return &openapi.Schema{Type: "string", Format: "uuid"}
	case strings.HasSuffix(name, "Number"):
		return &openapi.Schema{Type: "integer"}
	case name == "permission":
		return &openapi.Schema{Type: "string", Enum: enumStrings(perm.GlobalPermissions)}
	}
	return &openapi.Schema{Type: "string"}
}

func ok(body any) map[int]any      { return map[int]any{200: body} }
func created(body any) map[int]any { return map[int]any{201: body} }
func none() map[int]any            { return map[int]any{204: nil} }

// Route is one operation as the integration suite sees it: which request it
// answers, and whether its answer is a file or a redirect.
type Route struct {
	Method, Path, ID string
	Public, Binary   bool
	Redirect         bool
	// Pending operations answer 501 until they are built.
	Pending bool
	// OrgWide operations are refused a token limited to spaces.
	OrgWide bool
}

// Catalog lists every operation in the table.
func Catalog() []Route {
	out := make([]Route, 0, len(operations))
	for _, op := range operations {
		out = append(out, Route{Method: op.method, Path: op.path, ID: op.operationID(), Public: op.public, Binary: op.binary, Redirect: op.redirect, Pending: op.pending, OrgWide: op.orgWide})
	}
	return out
}

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
