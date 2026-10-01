package httpapi

import (
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/openapi"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/template"
	"github.com/praetorianer777/stator/backend/internal/theme"
	"github.com/praetorianer777/stator/backend/internal/watch"
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
	{method: "GET", path: "/auth/me", handler: "handleMe", tag: "auth", summary: "Who is signed in, and the organizations they may act in.",
		responses: ok(meResponse{})},
	{method: "POST", path: "/auth/switch-org", handler: "handleSwitchOrg", tag: "auth", summary: "Move the session to another organization.",
		request: switchOrgRequest{}, responses: ok(env{"organization": auth.CurrentOrg{}})},

	{method: "GET", path: "/oidc-provider", handler: "handleGetOIDCProvider", tag: "access", summary: "The organization's identity provider, if one is configured. For administrators.",
		responses: ok(providerView{})},
	{method: "PUT", path: "/oidc-provider", handler: "handleSaveOIDCProvider", tag: "access", summary: "Configure the organization's identity provider. For administrators.",
		request: saveOIDCProviderRequest{}, responses: ok(providerView{})},
	{method: "GET", path: "/oidc-provider/group-roles", handler: "handleListGroupRoles", tag: "access", summary: "Which provider groups grant which role. For administrators.",
		responses: ok(env{"groupRoles": []oidc.GroupRole{}})},
	{method: "POST", path: "/oidc-provider/group-roles", handler: "handleSetGroupRole", tag: "access", summary: "Map a provider group to a role, or change the role it maps to; members follow at their next sign-in. For administrators.",
		request: setGroupRoleRequest{}, responses: ok(env{"groupRole": oidc.GroupRole{}})},
	{method: "DELETE", path: "/oidc-provider/group-roles/{groupRoleID}", handler: "handleRemoveGroupRole", tag: "access", summary: "Unmap a provider group; the roles it granted go at each person's next sign-in. For administrators.",
		responses: none()},
	{method: "GET", path: "/users", handler: "handleListMembers", tag: "access", summary: "The organization's members, with their roles and whether the identity provider decides them. For administrators.",
		responses: ok(env{"members": []auth.Member{}})},
	{method: "DELETE", path: "/users/{userID}", handler: "handleRemoveMember", tag: "access", summary: "Take somebody out of the organization. The owner stays. For administrators.",
		responses: none()},
	{method: "GET", path: "/users/requests", handler: "handleListJoinRequests", tag: "access", summary: "Who signed in through the identity provider and is waiting to be let in. For administrators.",
		responses: ok(env{"requests": []auth.JoinRequest{}})},
	{method: "POST", path: "/users/requests/{userID}/admit", handler: "handleAdmitJoinRequest", tag: "access", summary: "Let a waiting person in with the standing given. For administrators.",
		request: admitRequest{}, responses: ok(env{"membership": auth.Membership{}})},
	{method: "DELETE", path: "/users/requests/{userID}", handler: "handleDeclineJoinRequest", tag: "access", summary: "Turn a waiting person away; they may ask again. For administrators.",
		responses: none()},
	{method: "GET", path: "/org/tokens", handler: "handleListOrgAPITokens", tag: "access", summary: "Every personal access token in the organization, with whose it is. For administrators.",
		responses: ok(env{"tokens": []auth.OrgAPIToken{}})},
	{method: "DELETE", path: "/org/tokens/{tokenID}", handler: "handleRevokeOrgAPIToken", tag: "access", summary: "Revoke anybody's token in the organization. For administrators.",
		responses: none()},

	{method: "GET", path: "/tokens", handler: "handleListAPITokens", tag: "tokens", summary: "The caller's personal access tokens in this organization, without their secrets.",
		responses: ok(env{"tokens": []auth.APIToken{}})},
	{method: "POST", path: "/tokens", handler: "handleCreateAPIToken", tag: "tokens", summary: "Make a personal access token; the secret is in this answer and never again. Needs a session.",
		request: createTokenRequest{}, responses: created(env{"token": auth.APIToken{}})},
	{method: "DELETE", path: "/tokens/{tokenID}", handler: "handleRevokeAPIToken", tag: "tokens", summary: "Revoke one of the caller's tokens; it stops working at once.",
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

	{method: "GET", path: "/spaces", handler: "handleListSpaces", tag: "spaces", summary: "Every space the caller may see, by name.", responses: ok(env{"spaces": []space.Space{}})},
	{method: "POST", path: "/spaces", handler: "handleCreateSpace", tag: "spaces", summary: "Make a space and its home page. For whoever may create spaces.", request: space.CreateInput{}, responses: created(env{"space": space.Space{}})},
	{method: "GET", path: "/spaces/{spaceKey}", handler: "handleGetSpace", tag: "spaces", summary: "One space by its key, and what the caller may do in it.", responses: ok(env{"space": space.Space{}})},
	{method: "PATCH", path: "/spaces/{spaceKey}", handler: "handleUpdateSpace", tag: "spaces", summary: "Rename or describe a space. For the space's administrators.", request: space.UpdateInput{}, responses: ok(env{"space": space.Space{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}", handler: "handleDeleteSpace", tag: "spaces", summary: "Delete a space and every page in it. For the space's administrators.", responses: none()},

	{method: "GET", path: "/spaces/{spaceKey}/pages", handler: "handleListPages", tag: "pages", summary: "The pages directly under a parent, by default under the space's home page, in order.",
		query: []param{{name: "parent", description: "The page whose children to list.", schema: &openapi.Schema{Type: "string", Format: "uuid"}}}, responses: ok(env{"pages": []page.TreeNode{}})},
	{method: "GET", path: "/spaces/{spaceKey}/outline", handler: "handleSpaceOutline", tag: "pages", summary: "Every page of a space in reading order, with its depth, for choosing where a page goes.", responses: ok(env{"pages": []page.OutlineEntry{}})},
	{method: "GET", path: "/spaces/{spaceKey}/trash", handler: "handleListTrash", tag: "trash", summary: "The space's trash, the latest first.", responses: ok(env{"items": []page.TrashItem{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}/trash", handler: "handleEmptyTrash", tag: "trash", summary: "Delete everything in the space's trash for good. For administrators.", responses: none()},
	{method: "POST", path: "/spaces/{spaceKey}/trash/{pageID}/restore", handler: "handleRestorePage", tag: "trash", summary: "Put a trashed page back where it was, or under the home page when that is gone.", responses: ok(env{"page": page.Page{}})},
	{method: "DELETE", path: "/spaces/{spaceKey}/trash/{pageID}", handler: "handlePurgePage", tag: "trash", summary: "Delete a trashed page and what went with it for good. For administrators.", responses: none()},
	{method: "POST", path: "/pages", handler: "handleCreatePage", tag: "pages", summary: "Add a page under a parent, last unless a place is named; unpublished and its creator's alone unless publish is set.", request: page.CreateInput{}, responses: created(env{"page": page.Page{}})},
	{method: "GET", path: "/pages/{pageID}", handler: "handleGetPage", tag: "pages", summary: "One page with its body, and the space it is in.", responses: ok(pageResponse{})},
	{method: "PATCH", path: "/pages/{pageID}", handler: "handleUpdatePage", tag: "pages", summary: "Publish a new title or body as the next version, with no comment, over the version it was made from; drafts are left alone.", request: page.UpdateInput{}, responses: ok(env{"page": page.Page{}})},
	{method: "DELETE", path: "/pages/{pageID}", handler: "handleTrashPage", tag: "pages", summary: "Move a page and every page below it to its space's trash.", responses: none()},
	{method: "POST", path: "/pages/{pageID}/move", handler: "handleMovePage", tag: "pages", summary: "Move a page under another, in its space or another, with or without its children; a move under itself is refused.", request: page.MoveInput{}, responses: ok(env{"page": page.Page{}})},
	{method: "POST", path: "/pages/{pageID}/copy", handler: "handleCopyPage", tag: "pages", summary: "Copy a page, with or without the pages below it, under a parent in its space or another.", request: page.CopyInput{}, responses: created(env{"page": page.Page{}})},
	{method: "GET", path: "/pages/{pageID}/below", handler: "handleListPagesBelow", tag: "pages", summary: "The pages under a page that the caller may view, out of the trash, each after its parent, for a child pages block; truncated says the list stopped at its limit.",
		query: []param{
			{name: "scope", schema: &openapi.Schema{Type: "string", Enum: document.ChildPagesScopes}, description: "children, the default, or subtree."},
			{name: "depth", schema: intParam, description: "For subtree, how many levels down, 1 to 10; every level when absent."},
			{name: "sort", schema: &openapi.Schema{Type: "string", Enum: document.ChildPagesSorts}, description: "How siblings are ordered: tree, the default, title, or updated, the latest change first."},
		}, responses: ok(env{"pages": []page.BelowPage{}, "truncated": false})},

	// Templates (#15).
	{method: "GET", path: "/templates", handler: "handleListTemplates", tag: "templates", summary: "The documents a new page can start from, in the order to offer them; send one's body and title with POST /pages.",
		responses: ok(env{"templates": []template.Template{}})},
	{method: "GET", path: "/templates/{templateKey}", handler: "handleGetTemplate", tag: "templates", summary: "One template by its key.",
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
	{method: "GET", path: "/pages/{pageID}/versions", handler: "handleListVersions", tag: "history", summary: "A page's published versions, the latest first.",
		query: pageQuery, responses: ok(env{"versions": []page.VersionEntry{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/pages/{pageID}/versions/{versionNumber}", handler: "handleGetVersion", tag: "history", summary: "One published version with its body, to read it as it was.",
		responses: ok(env{"version": page.Version{}})},
	{method: "POST", path: "/pages/{pageID}/versions/{versionNumber}/restore", handler: "handleRestoreVersion", tag: "history", summary: "Publish an older version's title and body again, as the next version.",
		request: page.RestoreInput{}, responses: map[int]any{200: env{"page": page.Page{}, "version": page.VersionEntry{}}, 409: errorEnvelope{}}},
	{method: "GET", path: "/pages/{pageID}/compare", handler: "handleCompareVersions", tag: "history", summary: "What changed between two versions of a page, or between a version and the caller's draft.",
		query: []param{
			{name: "from", description: "A version number, 0 for the empty page, or draft; when absent the version before to, or the draft's base version when to is draft."},
			{name: "to", description: "A version number, or draft; the latest version when absent."},
		}, responses: ok(env{"comparison": page.Comparison{}})},

	// Search (#18).
	{method: "GET", path: "/search", handler: "handleSearch", tag: "search", summary: "Pages, attachments and comments whose words match, among what the caller may see.",
		query: searchQuery, responses: ok(env{"hits": []search.Hit{}, "total": 0, "limit": 0, "offset": 0})},
	{method: "GET", path: "/search/quick", handler: "handleQuickSearch", tag: "search", summary: "Pages whose titles start with the words typed so far, for the top bar and the command palette.",
		query: []param{
			{name: "q", description: "The words typed so far."},
			{name: "space", description: "A space key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 20; 8 when absent."},
		}, responses: ok(env{"pages": []search.PageHit{}})},
	{method: "GET", path: "/recent-pages", handler: "handleRecentPages", tag: "search", summary: "The pages the caller visited last, the latest first.",
		query: []param{{name: "limit", schema: intParam, description: "1 to 20; 10 when absent."}}, responses: ok(env{"pages": []search.RecentPage{}})},
	{method: "POST", path: "/pages/{pageID}/visit", handler: "handleVisitPage", tag: "search", summary: "Note that the caller opened a page, for their recent pages.",
		responses: none()},

	// Labels (#17).
	{method: "GET", path: "/pages/{pageID}/labels", handler: "handleListPageLabels", tag: "labels", summary: "The labels on a page, by name.",
		responses: ok(env{"labels": []string{}})},
	{method: "POST", path: "/pages/{pageID}/labels", handler: "handleAddPageLabel", tag: "labels", summary: "Put a label on a page, normalized to one lower case word; one it carries already is no change.",
		request: label.LabelInput{}, responses: ok(env{"labels": []string{}})},
	{method: "DELETE", path: "/pages/{pageID}/labels/{labelName}", handler: "handleRemovePageLabel", tag: "labels", summary: "Take a label off a page; one it does not carry is no change.",
		responses: none()},
	{method: "GET", path: "/labels", handler: "handleSuggestLabels", tag: "labels", summary: "Labels on pages the caller may view that start with the words typed, the most used first.",
		query: []param{
			{name: "q", description: "What was typed so far; empty offers the most used labels."},
			{name: "space", description: "A space key to stay inside."},
			{name: "limit", schema: intParam, description: "1 to 50; 10 when absent."},
		}, responses: ok(env{"labels": []label.LabelSuggestion{}})},
	{method: "GET", path: "/labels/{labelName}/pages", handler: "handleListLabelPages", tag: "labels", summary: "The pages out of the trash that carry a label and that the caller may view, by title.",
		query:     append([]param{{name: "space", description: "A space key to stay inside; a space the caller may not view is not found."}}, pageQuery...),
		responses: ok(env{"pages": []label.LabeledPage{}, "total": 0, "limit": 0, "offset": 0})},

	// Permissions (#19).
	{method: "GET", path: "/access/me", handler: "handleMyAccess", tag: "permissions", summary: "What the caller may do across the organization, which decides which buttons to draw.",
		responses: ok(env{"can": perm.GlobalCan{}})},
	{method: "GET", path: "/org/permissions", handler: "handleListGlobalPermissions", tag: "permissions", summary: "Each global permission and whom it is granted to. For administrators.",
		responses: ok(env{"permissions": []perm.GlobalGrant{}})},
	{method: "PUT", path: "/org/permissions/{permission}", handler: "handleSetGlobalPermission", tag: "permissions", summary: "Replace whom a global permission is granted to. For administrators.",
		request: perm.GlobalGrantInput{}, responses: ok(env{"permission": perm.GlobalGrant{}})},
	{method: "GET", path: "/spaces/{spaceKey}/permissions", handler: "handleListSpacePermissions", tag: "permissions", summary: "Who may do what in a space. For the space's administrators.",
		responses: ok(env{"grants": []perm.SpaceGrant{}})},
	{method: "PUT", path: "/spaces/{spaceKey}/permissions", handler: "handleSetSpacePermissions", tag: "permissions", summary: "Replace a space's whole permission table. For the space's administrators.",
		request: perm.SpaceGrantsInput{}, responses: ok(env{"grants": []perm.SpaceGrant{}})},
	{method: "GET", path: "/pages/{pageID}/restrictions", handler: "handleGetPageRestrictions", tag: "permissions", summary: "Who may view and edit a page beyond the space's permissions, and the restricted pages above it.",
		responses: ok(env{"restrictions": page.Restrictions{}})},
	{method: "PUT", path: "/pages/{pageID}/restrictions", handler: "handleSetPageRestrictions", tag: "permissions", summary: "Replace a page's own view and edit restrictions; the pages below it inherit them.",
		request: page.RestrictionsInput{}, responses: ok(env{"restrictions": page.Restrictions{}})},
	{method: "GET", path: "/people", handler: "handleListPeople", tag: "permissions", summary: "Members of the organization, to pick whom to grant something.",
		query: pickerQuery, responses: ok(env{"people": []perm.Person{}})},
	{method: "GET", path: "/groups", handler: "handleListGroups", tag: "permissions", summary: "Groups of the organization, to pick whom to grant something.",
		query: pickerQuery, responses: ok(env{"groups": []perm.Group{}})},

	// Attachments (#20), as Armature serves them.
	{method: "GET", path: "/pages/{pageID}/attachments", handler: "handleListAttachments", tag: "attachments", summary: "The files on a page, the latest first.",
		responses: ok(env{"attachments": []attachment.Attachment{}})},
	{method: "POST", path: "/pages/{pageID}/attachments", handler: "handleUploadAttachment", tag: "attachments", summary: "Put a file on a page, as a multipart part named file; refused with too_large over the upload limit.", multipart: true,
		responses: map[int]any{201: env{"attachment": attachment.Attachment{}}, 413: errorEnvelope{}}},
	{method: "GET", path: "/attachments/{attachmentID}", handler: "handleDownloadAttachment", tag: "attachments", summary: "The bytes of a file, as a download.", binary: true,
		query: []param{{name: "inline", description: "1 to show images, PDFs and text in place."}}, responses: ok(nil)},
	{method: "DELETE", path: "/attachments/{attachmentID}", handler: "handleDeleteAttachment", tag: "attachments", summary: "Take a file off its page for good.",
		responses: none()},

	// Comments (#22) and inline comments (#23); see docs/api-contract-m2.md.
	{method: "GET", path: "/pages/{pageID}/comments", handler: "handleListComments", tag: "comments",
		summary:   "A page's threads with their comments, oldest first: below the page, inline, or both; resolved ones included.",
		query:     []param{{name: "kind", schema: &openapi.Schema{Type: "string", Enum: enumStrings(comment.Kinds)}, description: "page or inline; both when absent."}},
		responses: ok(env{"threads": []comment.Thread{}})},
	{method: "POST", path: "/pages/{pageID}/comments", handler: "handleStartThread", tag: "comments",
		summary: "Start a thread below a published page; refused with unpublished before its first publish.",
		request: comment.ThreadInput{}, responses: map[int]any{201: env{"thread": comment.Thread{}}, 409: errorEnvelope{}}},
	{method: "POST", path: "/pages/{pageID}/inline-comments", handler: "handleStartInlineThread", tag: "comments",
		summary: "Start a thread on a passage, sending the published body with the passage marked; refused with anchor_conflict when the body changed meanwhile.",
		request: comment.InlineThreadInput{}, responses: map[int]any{201: env{"thread": comment.Thread{}, "page": page.Page{}}, 409: errorEnvelope{}}},
	{method: "GET", path: "/comments/{commentID}", handler: "handleGetThread", tag: "comments",
		summary:   "The whole thread a comment belongs to, where a notification leads.",
		responses: ok(env{"thread": comment.Thread{}})},
	{method: "PATCH", path: "/comments/{commentID}", handler: "handleEditComment", tag: "comments",
		summary: "Rewrite one's own comment; nobody else may.",
		request: comment.BodyInput{}, responses: ok(env{"comment": comment.Comment{}})},
	{method: "DELETE", path: "/comments/{commentID}", handler: "handleDeleteComment", tag: "comments",
		summary:   "Delete one's own comment, or anybody's with the space's delete permission; its words go, its replies stay.",
		responses: none()},
	{method: "POST", path: "/comments/{commentID}/replies", handler: "handleReply", tag: "comments",
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

	// Mentions (#24).
	{method: "GET", path: "/pages/{pageID}/mentionable", handler: "handleListMentionable", tag: "mentions",
		summary: "Members to mention on a page, each saying whether they may view it once published; only those are told.",
		query:   pickerQuery, responses: ok(env{"people": []perm.Mentionable{}})},

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

	// Notifications (#26), as Armature serves them.
	{method: "GET", path: "/notifications", handler: "handleListNotifications", tag: "notifications",
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
	{method: "GET", path: "/armature/connection", handler: "handleGetArmatureConnection", tag: "armature",
		summary:   "The organization's Armature instance and what to enter in Armature's webhook settings, or null. For administrators.",
		responses: ok(env{"connection": (*armature.Connection)(nil)})},
	{method: "PUT", path: "/armature/connection", handler: "handleSaveArmatureConnection", tag: "armature",
		summary: "Connect an Armature instance; a new address or organization forgets every stored token. For administrators.",
		request: armature.ConnectionInput{}, responses: ok(env{"connection": armature.Connection{}})},
	{method: "DELETE", path: "/armature/connection", handler: "handleRemoveArmatureConnection", tag: "armature",
		summary:   "Disconnect Armature, forgetting every stored token and the webhook secret. For administrators.",
		responses: none()},
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

// Spec builds the OpenAPI document from the table.
func Spec() *openapi.Document {
	b := openapi.NewBuilder()
	b.FieldOverrides["Backdrop.fit"] = &openapi.Schema{Type: "string", Enum: theme.BackdropFits}
	scopes := &openapi.Schema{Type: "array", Items: &openapi.Schema{Type: "string", Enum: []string{auth.ScopeRead}}}
	b.FieldOverrides["APIToken.scopes"] = scopes
	b.FieldOverrides["OrgAPIToken.scopes"] = scopes
	b.FieldOverrides["CreateTokenRequest.scopes"] = scopes
	b.Enums[reflect.TypeOf(auth.OrgRole(""))] = enumStrings(auth.OrgRoles)
	b.Enums[reflect.TypeOf(auth.RoleSource(""))] = enumStrings(auth.RoleSources)
	b.Enums[reflect.TypeOf(perm.SubjectType(""))] = enumStrings(perm.SubjectTypes)
	b.Enums[reflect.TypeOf(perm.GlobalPermission(""))] = enumStrings(perm.GlobalPermissions)
	b.Enums[reflect.TypeOf(perm.SpacePermission(""))] = enumStrings(perm.SpacePermissions)
	b.Enums[reflect.TypeOf(page.DiffChange(""))] = enumStrings(page.DiffChanges)
	b.Enums[reflect.TypeOf(search.HitType(""))] = enumStrings(search.HitTypes)
	b.Enums[reflect.TypeOf(comment.Kind(""))] = enumStrings(comment.Kinds)
	b.Enums[reflect.TypeOf(comment.AnchorState(""))] = enumStrings(comment.AnchorStates)
	b.Enums[reflect.TypeOf(watch.Kind(""))] = enumStrings(watch.Kinds)
	b.Enums[reflect.TypeOf(notify.Kind(""))] = enumStrings(notify.Kinds)
	b.Enums[reflect.TypeOf(notify.Digest(""))] = enumStrings(notify.Digests)
	b.Enums[reflect.TypeOf(armature.Status(""))] = enumStrings(armature.Statuses)
	b.Enums[reflect.TypeOf(armature.LinkState(""))] = enumStrings(armature.LinkStates)
	b.FieldOverrides["Issue.priority"] = &openapi.Schema{Type: "string", Enum: armature.Priorities}
	b.FieldOverrides["IssueStatus.category"] = &openapi.Schema{Type: "string", Enum: armature.StatusCategories}
	// A group grants member or admin; owner is never the provider's to give.
	granted := &openapi.Schema{Type: "string", Enum: []string{string(auth.RoleAdmin), string(auth.RoleMember)}}
	b.FieldOverrides["GroupRole.role"] = granted
	b.FieldOverrides["SetGroupRoleRequest.role"] = granted

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
					Description: "A personal access token, made under Tokens in the account menu; a token with the read scope is refused every write."},
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
}

// Catalog lists every operation in the table.
func Catalog() []Route {
	out := make([]Route, 0, len(operations))
	for _, op := range operations {
		out = append(out, Route{Method: op.method, Path: op.path, ID: op.operationID(), Public: op.public, Binary: op.binary, Redirect: op.redirect, Pending: op.pending})
	}
	return out
}

// handleOpenAPI serves the document this process was built from.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, Spec())
}
