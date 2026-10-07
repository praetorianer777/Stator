package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/calendar"
	"github.com/praetorianer777/stator/backend/internal/collab"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/guest"
	"github.com/praetorianer777/stator/backend/internal/home"
	"github.com/praetorianer777/stator/backend/internal/hub"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/mdio"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/observability"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/pageview"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/public"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/render"
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

// readinessTimeout bounds the database round trip behind /readyz, so a probe
// gets an answer before its own deadline does.
const readinessTimeout = 3 * time.Second

// APIPrefix is where every endpoint but the probes lives.
const APIPrefix = "/api/v1"

// Database is what the handlers ask of the cluster; *db.Cluster is the real one.
type Database interface {
	Ping(ctx context.Context) error
	Stats() db.Stats
}

// Server holds everything the handlers need.
type Server struct {
	DB   Database
	Auth Authenticator
	// Accounts signs people in and out; OIDC runs sign-in through a provider.
	// Both nil answer every sign-in route that it is not set up.
	Accounts *auth.Service
	OIDC     *oidc.Service
	// OIDCCallbackURL is the address to register with a provider.
	OIDCCallbackURL string
	Log             *slog.Logger
	// Telemetry counts and traces requests; nil serves without either.
	Telemetry *observability.Telemetry
	Themes    *theme.Service
	Spaces    *space.Service
	Pages     *page.Service
	Search    *search.Service
	Labels    *label.Service
	Comments  *comment.Service
	Reactions *reaction.Service
	// Watches keeps who follows which pages and spaces; Notifications what
	// they were told and how they want to hear.
	Watches       *watch.Service
	Notifications *notify.Service
	// Stars keeps each person's starred pages and spaces; Home reads the
	// home page's lists of updates and edits.
	Stars *star.Service
	Home  *home.Service
	// Templates keeps the organization's own templates beside the built-ins.
	Templates *template.Service
	// Stale reads the stale content report for administrators.
	Stale *stale.Service
	// Tasks reads the tasks people are assigned on published pages.
	Tasks *task.Service
	// Shares sends pages to people who may read them, with a note.
	Shares *share.Service
	// Shortcuts keeps the links pinned above each space's page tree.
	Shortcuts *shortcut.Service
	// Calendars keeps each space's calendars and their events.
	Calendars *calendar.Service
	// Guests lets people from outside into one space each.
	Guests *guest.Service
	// Public serves the spaces anybody may read without signing in, and the
	// organization's switch for it; nil answers that nothing is public.
	Public *public.Service
	Hub    *hub.Service
	Unfurl *unfurl.Service
	// PageViews reads how often pages were read and by whom;
	// PageViewRetention is how long the worker keeps named views, zero forever.
	PageViews         *pageview.Service
	PageViewRetention time.Duration
	// Perms answers the permission screens and the use check in front of
	// every route; nil lets everybody who is a member through.
	Perms *perm.Service
	// Attachments keeps the files on pages; nil answers that storage is off.
	Attachments *attachment.Service
	// Markdown imports and exports pages; nil makes one of Pages and Attachments.
	Markdown *mdio.Service
	// Renderer prints pages as PDF; nil answers that PDF export is not set up.
	Renderer render.Renderer
	// Armature keeps the organization's connection and the members' tokens;
	// nil answers that Armature is out of reach.
	Armature *armature.Service
	// Audit reads the record for administrators and keeps exports; nil refuses
	// both. AuditRetention is how long the worker keeps an entry, zero forever.
	Audit          *audit.Service
	AuditRetention time.Duration
	// Collab relays the shared drafts of pages being edited together; nil
	// answers that editing together is off, and the editor edits alone.
	Collab *collab.Hub
	// Webhooks keeps where the organization's events are posted; nil answers
	// that webhooks are not set up.
	Webhooks *webhook.Service
	// Fresh remembers each caller's last write between requests; nil leaves
	// reads unpinned, which is only right without replicas.
	Fresh Freshness
	// TestOrgs serves /test/orgs for the browser suite when it is set, to
	// callers with TestToken; nil leaves those paths unrouted.
	TestOrgs  TestOrgs
	TestToken string

	CookieName string
	Secure     bool
	// AppBaseURL is the web client's origin, which the same-site check trusts.
	AppBaseURL string
	// CheckOrigin also refuses a cookie-carried write from a foreign origin;
	// off in development, where the dev server proxies from an origin of its own.
	CheckOrigin bool
	// RequestTimeout bounds each handler; zero means the configured default.
	RequestTimeout time.Duration

	// handler is the router Routes built, which MCP tool calls run through.
	handler http.Handler
}

// Routes builds the HTTP surface. The order matters: an id first so every later
// line can quote it, recovery inside the logger so a panic still logs a line.
func (s *Server) Routes(allowedOrigins []string) http.Handler {
	timeout := s.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultRequestTimeout
	}

	r := chi.NewRouter()
	r.Use(requestID)
	if s.Telemetry != nil {
		r.Use(observe(s.Telemetry.Metrics))
	}
	r.Use(logging(s.Log))
	r.Use(recovery)
	r.Use(securityHeaders)
	r.Use(cors(allowedOrigins))
	r.Use(s.sameSite(allowedOrigins))
	r.Use(unlessUpgrade(middleware.Timeout(timeout)))
	r.Use(s.authenticate)
	r.Use(readOnlyToken)
	r.Use(s.readYourWrites)

	// Liveness and readiness are deliberately outside the API and authentication.
	r.Get("/healthz", s.handleLiveness)
	r.Get("/readyz", s.handleReadiness)

	r.Route(APIPrefix, func(r chi.Router) {
		r.Get("/openapi.json", s.handleOpenAPI)

		r.Post("/auth/login", s.handleLogin)
		r.Get("/auth/oidc/{orgSlug}/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Post("/armature/webhook/{orgSlug}", s.handleArmatureWebhook)
		r.Group(func(r chi.Router) {
			r.Use(s.anonymousReader)
			r.Get("/public/{orgSlug}", s.handlePublicSite)
			r.Get("/public/{orgSlug}/spaces/{spaceKey}", s.handlePublicSpace)
			r.Get("/public/{orgSlug}/pages/{pageID}", s.handlePublicPage)
			r.Get("/public/{orgSlug}/pages/{pageID}/pdf", s.handlePublicPagePDF)
			r.Get("/public/{orgSlug}/attachments/{attachmentID}", s.handlePublicAttachment)
			r.Get("/public/{orgSlug}/search", s.handlePublicSearch)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.linkReader)
			r.Get("/public/{orgSlug}/links/{token}", s.handleLinkedPage)
			r.Get("/public/{orgSlug}/links/{token}/pdf", s.handleLinkedPagePDF)
			r.Get("/public/{orgSlug}/links/{token}/attachments/{attachmentID}", s.handleLinkedAttachment)
		})
		mountPending(r, true)

		r.Group(func(r chi.Router) {
			r.Use(requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Patch("/auth/me", s.handleUpdateMe)
			r.Post("/auth/switch-org", s.handleSwitchOrg)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Get("/oidc-provider", s.handleGetOIDCProvider)
			r.Put("/oidc-provider", s.handleSaveOIDCProvider)
			r.Get("/oidc-provider/group-roles", s.handleListGroupRoles)
			r.Post("/oidc-provider/group-roles", s.handleSetGroupRole)
			r.Delete("/oidc-provider/group-roles/{groupRoleID}", s.handleRemoveGroupRole)
			r.Get("/users", s.handleListMembers)
			r.Delete("/users/{userID}", s.handleRemoveMember)
			r.Get("/users/requests", s.handleListJoinRequests)
			r.Post("/users/requests/{userID}/admit", s.handleAdmitJoinRequest)
			r.Delete("/users/requests/{userID}", s.handleDeclineJoinRequest)
			r.Get("/org/tokens", s.handleListOrgAPITokens)
			r.Delete("/org/tokens/{tokenID}", s.handleRevokeOrgAPIToken)
			r.Get("/org/permissions", s.handleListGlobalPermissions)
			r.Put("/org/permissions/{permission}", s.handleSetGlobalPermission)
			r.Put("/org/hub", s.handleSetHub)
			r.Get("/org/anonymous-access", s.handleGetAnonymousAccess)
			r.Put("/org/anonymous-access", s.handleSetAnonymousAccess)
			r.Get("/org/public-links", s.handleGetPublicLinks)
			r.Put("/org/public-links", s.handleSetPublicLinks)
			r.Get("/spaces/{spaceKey}/guests", s.handleListGuests)
			r.Post("/spaces/{spaceKey}/guests", s.handleInviteGuest)
			r.Delete("/spaces/{spaceKey}/guests/{userID}", s.handleRemoveGuest)
		})

		// What the caller may do is theirs to read even without use, so the
		// client can say why everything else is refused.
		r.With(requireOrg).Get("/access/me", s.handleMyAccess)

		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse, requireWholeOrg)
			r.Get("/tokens", s.handleListAPITokens)
			// Making one is for a session only, so a leaked token cannot mint a
			// longer lived one and outlive its own revocation.
			r.With(requireSession).Post("/tokens", s.handleCreateAPIToken)
			r.Delete("/tokens/{tokenID}", s.handleRevokeAPIToken)
		})

		// Themes are a person's in an organization; the examples are anybody's
		// signed in. The fixed paths come first so "active" is never an id.
		r.With(requireAuth).Get("/themes/examples", s.handleThemeExamples)
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse)
			r.Get("/themes", s.handleListThemes)
			r.Post("/themes", s.handleCreateTheme)
			r.Get("/themes/active", s.handleActiveTheme)
			r.Put("/themes/active", s.handleChooseTheme)
			r.Get("/themes/default", s.handleDefaultTheme)
			r.Put("/themes/default", s.handleSetDefaultTheme)
			r.Post("/themes/import", s.handleImportTheme)
			r.Get("/themes/{themeID}/export", s.handleExportTheme)
			r.Get("/themes/{themeID}", s.handleGetTheme)
			r.Patch("/themes/{themeID}", s.handleUpdateTheme)
			r.Delete("/themes/{themeID}", s.handleDeleteTheme)
			r.Post("/themes/{themeID}/assets", s.handleUploadThemeAsset)
			r.Get("/themes/{themeID}/assets/{assetID}", s.handleThemeAsset)
			r.Delete("/themes/{themeID}/assets/{assetID}", s.handleDeleteThemeAsset)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse, requireAdmin)
			r.Get("/armature/connection", s.handleGetArmatureConnection)
			r.Put("/armature/connection", s.handleSaveArmatureConnection)
			r.Delete("/armature/connection", s.handleRemoveArmatureConnection)
			r.Get("/audit", s.handleListAudit)
			r.Get("/audit/facets", s.handleAuditFacets)
			r.Get("/audit/export", s.handleExportAudit)
			r.Get("/webhooks", s.handleListWebhooks)
			r.Post("/webhooks", s.handleCreateWebhook)
			r.Patch("/webhooks/{webhookID}", s.handleUpdateWebhook)
			r.Delete("/webhooks/{webhookID}", s.handleDeleteWebhook)
			r.Post("/webhooks/{webhookID}/rotate-secret", s.handleRotateWebhookSecret)
			r.Post("/webhooks/{webhookID}/test", s.handleTestWebhook)
			r.Get("/webhooks/{webhookID}/deliveries", s.handleListWebhookDeliveries)
			r.Post("/webhooks/{webhookID}/deliveries/{deliveryID}/redeliver", s.handleRedeliverWebhook)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse)
			r.Get("/armature/account", s.handleGetArmatureAccount)
			r.Put("/armature/account/token", s.handleConnectArmatureAccount)
			r.Post("/armature/account/check", s.handleCheckArmatureAccount)
			r.Delete("/armature/account/token", s.handleDisconnectArmatureAccount)
			r.Get("/armature/issues", s.handleLookupArmatureIssues)
			r.Get("/armature/issues/{issueKey}", s.handleGetArmatureIssue)
			r.Get("/armature/projects", s.handleListArmatureProjects)
			r.Get("/armature/search", s.handleSearchArmatureIssues)
			r.Get("/armature/chart", s.handleArmatureChart)
			r.Get("/armature/roadmap", s.handleArmatureRoadmap)
			r.Get("/armature/calendar", s.handleArmatureCalendar)
			r.Get("/armature/issue-types", s.handleListArmatureIssueTypes)
			r.Post("/armature/issues", s.handleCreateArmatureIssues)
			r.Get("/pages/{pageID}/armature-links", s.handleListArmatureLinks)
			r.Get("/armature/theme", s.handleArmatureThemeFollow)
			r.Put("/armature/theme", s.handleFollowArmatureTheme)
			r.Delete("/armature/theme", s.handleUnfollowArmatureTheme)
		})

		if s.TestOrgs != nil {
			r.Group(func(r chi.Router) {
				r.Use(s.requireTestToken)
				r.Post("/test/orgs", s.handleCreateTestOrg)
				r.Delete("/test/orgs/{orgSlug}", s.handleDeleteTestOrg)
			})
		}

		r.Group(func(r chi.Router) {
			r.Use(requireOrg, s.requireUse)
			r.Get("/people", s.handleListPeople)
			r.Get("/groups", s.handleListGroups)
			r.Get("/spaces", s.handleListSpaces)
			// A new space is outside every space a limited token names.
			r.With(requireWholeOrg).Post("/spaces", s.handleCreateSpace)
			r.With(requireWholeOrg).Get("/example-space", s.handleGetExampleSpace)
			r.With(requireWholeOrg).Post("/example-space", s.handleCreateExampleSpace)
			r.Get("/spaces/{spaceKey}", s.handleGetSpace)
			r.Patch("/spaces/{spaceKey}", s.handleUpdateSpace)
			r.Delete("/spaces/{spaceKey}", s.handleDeleteSpace)
			r.Get("/spaces/{spaceKey}/pages", s.handleListPages)
			r.Get("/spaces/{spaceKey}/outline", s.handleSpaceOutline)
			r.Get("/spaces/{spaceKey}/decisions", s.handleListDecisions)
			r.Get("/spaces/{spaceKey}/blog", s.handleGetBlog)
			r.Post("/spaces/{spaceKey}/posts", s.handleCreatePost)
			r.Get("/spaces/{spaceKey}/permissions", s.handleListSpacePermissions)
			r.Put("/spaces/{spaceKey}/permissions", s.handleSetSpacePermissions)
			r.Get("/spaces/{spaceKey}/permissions/copy", s.handlePreviewPermissionCopy)
			r.Post("/spaces/{spaceKey}/permissions/copy", s.handleCopyPermissions)
			r.Get("/spaces/{spaceKey}/anonymous-access", s.handleGetSpaceAnonymousAccess)
			r.Put("/spaces/{spaceKey}/anonymous-access", s.handleSetSpaceAnonymousAccess)
			r.Put("/spaces/{spaceKey}/archive", s.handleArchiveSpace)
			r.Delete("/spaces/{spaceKey}/archive", s.handleUnarchiveSpace)
			r.Get("/spaces/{spaceKey}/archived-pages", s.handleListArchivedPages)
			r.Get("/spaces/{spaceKey}/shortcuts", s.handleListShortcuts)
			r.Post("/spaces/{spaceKey}/shortcuts", s.handleCreateShortcut)
			r.Post("/spaces/{spaceKey}/shortcuts/{shortcutID}/move", s.handleMoveShortcut)
			r.Delete("/spaces/{spaceKey}/shortcuts/{shortcutID}", s.handleDeleteShortcut)
			r.Get("/spaces/{spaceKey}/calendars", s.handleListCalendars)
			r.Post("/spaces/{spaceKey}/calendars", s.handleCreateCalendar)
			r.Patch("/calendars/{calendarID}", s.handleRenameCalendar)
			r.Delete("/calendars/{calendarID}", s.handleDeleteCalendar)
			r.Get("/calendars/{calendarID}/events", s.handleListCalendarEvents)
			r.Post("/calendars/{calendarID}/events", s.handleCreateCalendarEvent)
			r.Put("/calendars/{calendarID}/events/{eventID}", s.handleUpdateCalendarEvent)
			r.Delete("/calendars/{calendarID}/events/{eventID}", s.handleDeleteCalendarEvent)
			r.Get("/spaces/{spaceKey}/trash", s.handleListTrash)
			r.Delete("/spaces/{spaceKey}/trash", s.handleEmptyTrash)
			r.Post("/spaces/{spaceKey}/trash/{pageID}/restore", s.handleRestorePage)
			r.Delete("/spaces/{spaceKey}/trash/{pageID}", s.handlePurgePage)
			r.Post("/pages", s.handleCreatePage)
			r.Get("/pages/{pageID}", s.handleGetPage)
			r.Get("/pages/{pageID}/excerpts", s.handleListExcerpts)
			r.Get("/pages/{pageID}/included", s.handleGetIncluded)
			r.Patch("/pages/{pageID}", s.handleUpdatePage)
			r.Delete("/pages/{pageID}", s.handleTrashPage)
			r.Post("/pages/{pageID}/move", s.handleMovePage)
			r.Post("/pages/{pageID}/copy", s.handleCopyPage)
			r.Put("/pages/{pageID}/archive", s.handleArchivePage)
			r.Delete("/pages/{pageID}/archive", s.handleUnarchivePage)
			r.Get("/pages/{pageID}/below", s.handleListPagesBelow)
			r.Get("/pages/{pageID}/export", s.handleExportPage)
			r.Get("/pages/{pageID}/markdown", s.handleGetPageMarkdown)
			r.Get("/pages/{pageID}/pdf", s.handlePagePDF)
			r.Put("/pages/{pageID}/markdown", s.handleReplacePageMarkdown)
			r.Post("/pages/{pageID}/import", s.handleImportMarkdown)
			r.Get("/templates", s.handleListTemplates)
			r.Post("/templates", s.handleCreateTemplate)
			r.Get("/templates/{templateKey}", s.handleGetTemplate)
			r.Put("/templates/{templateKey}", s.handleUpdateTemplate)
			r.Delete("/templates/{templateKey}", s.handleDeleteTemplate)
			r.Get("/space-templates", s.handleListSpaceTemplates)
			r.Get("/pages/{pageID}/labels", s.handleListPageLabels)
			r.Post("/pages/{pageID}/labels", s.handleAddPageLabel)
			r.Delete("/pages/{pageID}/labels/{labelName}", s.handleRemovePageLabel)
			r.Get("/labels", s.handleSuggestLabels)
			r.Get("/labels/{labelName}/pages", s.handleListLabelPages)
			r.Get("/properties-report", s.handlePropertiesReport)
			r.Get("/labelled-pages", s.handleLabelledPages)
			r.Get("/updated-pages", s.handleUpdatedPages)
			r.Get("/posts", s.handleListPosts)
			r.Get("/task-report", s.handleTaskReport)
			r.Get("/template-button", s.handleTemplateButton)
			r.Post("/templates/{templateKey}/pages", s.handleCreateFromTemplate)
			r.Get("/pages/{pageID}/contributors", s.handlePageContributors)
			r.Get("/pages/{pageID}/attachments", s.handleListAttachments)
			r.Post("/pages/{pageID}/attachments", s.handleUploadAttachment)
			r.Get("/attachments/{attachmentID}", s.handleDownloadAttachment)
			r.Get("/attachments/{attachmentID}/preview", s.handlePreviewAttachment)
			r.Delete("/attachments/{attachmentID}", s.handleDeleteAttachment)
			r.Post("/attachments/{attachmentID}/restore", s.handleRestoreAttachment)
			r.Post("/attachments/{attachmentID}/edit", s.handleEditAttachment)
			r.Get("/pages/{pageID}/draft", s.handleGetDraft)
			r.Put("/pages/{pageID}/draft", s.handleSaveDraft)
			r.Delete("/pages/{pageID}/draft", s.handleDiscardDraft)
			// A browser holds it open while its person edits, so a token has no use for it.
			r.With(requireSession).Get("/pages/{pageID}/collab", s.handleCollab)
			r.Post("/pages/{pageID}/publish", s.handlePublishPage)
			r.Put("/pages/{pageID}/schedule", s.handleSchedulePublish)
			r.Delete("/pages/{pageID}/schedule", s.handleCancelSchedule)
			r.Put("/pages/{pageID}/live", s.handleSaveLive)
			r.Put("/pages/{pageID}/mode", s.handleSetPageMode)
			r.Get("/pages/{pageID}/versions", s.handleListVersions)
			r.Get("/pages/{pageID}/versions/{versionNumber}", s.handleGetVersion)
			r.Post("/pages/{pageID}/versions/{versionNumber}/restore", s.handleRestoreVersion)
			r.Get("/pages/{pageID}/compare", s.handleCompareVersions)
			r.Get("/pages/{pageID}/restrictions", s.handleGetPageRestrictions)
			r.Put("/pages/{pageID}/restrictions", s.handleSetPageRestrictions)
			r.Post("/pages/{pageID}/restrictions/check", s.handleCheckPageRestrictions)
			r.Get("/pages/{pageID}/access/{userID}", s.handleInspectPageAccess)
			r.Post("/pages/{pageID}/visit", s.handleVisitPage)
			r.Get("/pages/{pageID}/views", s.handlePageViews)
			r.Get("/pages/{pageID}/readers", s.handlePageReaders)
			r.Get("/pages/{pageID}/comments", s.handleListComments)
			r.Post("/pages/{pageID}/comments", s.handleStartThread)
			r.Get("/comments/{commentID}", s.handleGetThread)
			r.Patch("/comments/{commentID}", s.handleEditComment)
			r.Delete("/comments/{commentID}", s.handleDeleteComment)
			r.Post("/comments/{commentID}/replies", s.handleReply)
			r.Post("/pages/{pageID}/inline-comments", s.handleStartInlineThread)
			r.Post("/comments/{commentID}/resolve", s.handleResolveThread)
			r.Post("/comments/{commentID}/reopen", s.handleReopenThread)
			r.Post("/pages/{pageID}/reactions", s.handleReactToPage)
			r.Delete("/pages/{pageID}/reactions", s.handleUnreactPage)
			r.Post("/comments/{commentID}/reactions", s.handleReactToComment)
			r.Delete("/comments/{commentID}/reactions", s.handleUnreactComment)
			r.Get("/search", s.handleSearch)
			r.Get("/search/quick", s.handleQuickSearch)
			r.Get("/recent-pages", s.handleRecentPages)
			r.Put("/pages/{pageID}/watch", s.handleWatchPage)
			r.Delete("/pages/{pageID}/watch", s.handleUnwatchPage)
			r.Get("/pages/{pageID}/watchers", s.handleListWatchers)
			r.Get("/pages/{pageID}/mentionable", s.handleListMentionable)
			r.Post("/pages/{pageID}/share", s.handleSharePage)
			r.Get("/pages/{pageID}/share/recipients", s.handleShareRecipients)
			r.Get("/pages/{pageID}/public-links", s.handleListPageLinks)
			r.Post("/pages/{pageID}/public-links", s.handleCreatePageLink)
			r.Delete("/pages/{pageID}/public-links/{linkID}", s.handleRevokePageLink)
			r.Get("/pages/{pageID}/viewers", s.handleListViewers)
			r.Put("/spaces/{spaceKey}/watch", s.handleWatchSpace)
			r.Delete("/spaces/{spaceKey}/watch", s.handleUnwatchSpace)
			r.Put("/spaces/{spaceKey}/blog/watch", s.handleWatchBlog)
			r.Delete("/spaces/{spaceKey}/blog/watch", s.handleUnwatchBlog)
			r.Get("/watches", s.handleListWatches)
			r.Put("/pages/{pageID}/star", s.handleStarPage)
			r.Delete("/pages/{pageID}/star", s.handleUnstarPage)
			r.Put("/spaces/{spaceKey}/star", s.handleStarSpace)
			r.Delete("/spaces/{spaceKey}/star", s.handleUnstarSpace)
			r.Get("/stars", s.handleListStars)
			r.Get("/home/updates", s.handleHomeUpdates)
			r.Get("/org/hub", s.handleGetHub)
			r.Get("/link-preview", s.handleLinkPreview)
			r.Get("/home/edited", s.handleHomeEdited)
			r.Get("/stale-pages", s.handleListStalePages)
			r.Get("/tasks", s.handleListMyTasks)
			r.Patch("/pages/{pageID}/tasks/{taskID}", s.handleSetTaskDone)
			r.Put("/pages/{pageID}/owner", s.handleSetPageOwner)
			r.Put("/pages/{pageID}/appearance", s.handleSetAppearance)
			r.Delete("/pages/{pageID}/owner", s.handleRemovePageOwner)
			r.Put("/pages/{pageID}/verification", s.handleVerifyPage)
			r.Delete("/pages/{pageID}/verification", s.handleUnverifyPage)
			r.Get("/notifications", s.handleListNotifications)
			r.Get("/notifications/unread-count", s.handleUnreadCount)
			r.Post("/notifications/read", s.handleMarkRead)
			r.Get("/notification-preferences", s.handleNotificationPreferences)
			r.Put("/notification-preferences", s.handleSaveNotificationPreferences)
			r.Post("/mcp", s.handleMCP)
			mountPending(r, false)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, ErrNotFound("No such endpoint. The API lives under "+APIPrefix+"."))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		respondError(w, r, &APIError{
			Status:  http.StatusMethodNotAllowed,
			Code:    "method_not_allowed",
			Message: "That method is not allowed here.",
		})
	})
	s.handler = r
	return r
}

// errNotImplemented answers an operation the API describes but does not serve yet.
var errNotImplemented = &APIError{Status: http.StatusNotImplemented, Code: "not_implemented",
	Message: "This part of the API is not built yet. Update Stator to a release that has it, or leave it out for now."}

// mountPending routes each pending operation to a 501 so clients can be written
// against it: public ones open, the rest behind the same guards as the pages.
func mountPending(r chi.Router, public bool) {
	for _, op := range operations {
		if op.pending && op.public == public {
			r.Method(op.method, op.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respondError(w, r, errNotImplemented)
			}))
		}
	}
}

// statusResponse is what the probes answer.
type statusResponse struct {
	Status string `json:"status"`
}

// readinessResponse adds how reads are being routed, for an operator.
type readinessResponse struct {
	Status  string   `json:"status"`
	Routing db.Stats `json:"routing"`
}

// handleLiveness touches nothing external on purpose: a database outage must
// not get the container killed and restarted, which would only make it worse.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, statusResponse{Status: "ok"})
}

// handleReadiness answers whether the process can serve traffic. Nobody has
// signed in to read it, so a failure says whether, not why.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	if s.DB == nil {
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	if err := s.DB.Ping(ctx); err != nil {
		loggerFrom(r.Context()).Error("not ready", "error", err)
		respondJSON(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	respondJSON(w, r, http.StatusOK, readinessResponse{Status: "ok", Routing: s.DB.Stats()})
}
