// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/hub"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/mdio"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/share"
	"github.com/praetorianer777/stator/backend/internal/shortcut"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/tenant"
	"github.com/praetorianer777/stator/backend/internal/theme"
	"github.com/praetorianer777/stator/backend/internal/unfurl"
	"github.com/praetorianer777/stator/backend/internal/watch"
	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// APIError is the single error shape every endpoint returns, so that clients
// have exactly one thing to parse.
type APIError struct {
	// Status is the HTTP status code; it is not serialised.
	Status int `json:"-"`
	// Code is a stable machine readable identifier such as "not_found".
	Code string `json:"code"`
	// Message is a sentence a person can read and act on.
	Message string `json:"message"`
	// Fields carries per-field validation messages keyed by field name.
	Fields map[string]string `json:"fields,omitempty"`
	// Position is the 1-based character an NQL query went wrong at, as
	// Armature reports it with bad_query.
	Position *int `json:"position,omitempty"`
	// RequestID lets a user quote something we can find in the logs.
	RequestID string `json:"requestId,omitempty"`

	// cause is logged but never sent to the client.
	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Unwrap() error { return e.cause }

// errorEnvelope is how every failure is written.
type errorEnvelope struct {
	Error APIError `json:"error"`
}

func ErrBadRequest(message string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}

func ErrValidation(fields map[string]string) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Some fields need attention.",
		Fields:  fields,
	}
}

func ErrUnauthorized(message string) *APIError {
	if message == "" {
		message = "Sign in to continue."
	}
	return &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: message}
}

func ErrForbidden(message string) *APIError {
	if message == "" {
		message = "You do not have permission to do that."
	}
	return &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: message}
}

// ErrNotFound is also what a caller gets for a resource in another tenant:
// existence itself is privileged information.
func ErrNotFound(what string) *APIError {
	if what == "" {
		what = "That was not found."
	}
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: what}
}

func ErrConflict(message string) *APIError {
	return &APIError{Status: http.StatusConflict, Code: "conflict", Message: message}
}

func ErrInternal(cause error) *APIError {
	return &APIError{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "Something went wrong on our side. Try again, and quote the request id if it keeps happening.",
		cause:   cause,
	}
}

var (
	errReadOnlyToken = &APIError{Status: http.StatusForbidden, Code: "read_only_token",
		Message: "This token can only read. Use a token without the read scope, or sign in, to make changes."}
	errSessionOnly = &APIError{Status: http.StatusForbidden, Code: "session_only",
		Message: "A token cannot do this. Sign in to Stator and do it there."}
	errSpacesToken = &APIError{Status: http.StatusForbidden, Code: "spaces_token",
		Message: "This token is limited to some spaces, and this concerns the whole organization. Use a token without that limit, or sign in."}
)

// The database's names for its refusals of a folder's content and of a change of kind.
const (
	folderConstraint = "page_is_folder"
	kindConstraint   = "page_kind_fixed"
)

// toAPIError maps a domain error onto the wire shape. One place for it is what
// stops handlers leaking internals into responses by accident.
func toAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	// The database holds what a folder may not have whichever service asks,
	// so its refusal reads as the service's would.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.ConstraintName == folderConstraint || pgErr.ConstraintName == kindConstraint) {
		err = page.ErrFolder
	}
	var invalid *oidc.ValidationError
	if errors.As(err, &invalid) {
		return ErrValidation(map[string]string{invalid.Field: invalid.Message})
	}
	var spaceField *space.FieldError
	if errors.As(err, &spaceField) {
		return ErrValidation(map[string]string{spaceField.Field: spaceField.Message})
	}
	var pageField *page.FieldError
	if errors.As(err, &pageField) {
		return ErrValidation(map[string]string{pageField.Field: pageField.Message})
	}
	var permField *perm.FieldError
	if errors.As(err, &permField) {
		return ErrValidation(map[string]string{permField.Field: permField.Message})
	}
	var labelField *label.FieldError
	if errors.As(err, &labelField) {
		return ErrValidation(map[string]string{labelField.Field: sentence(labelField.Message)})
	}
	var searchField *search.FieldError
	if errors.As(err, &searchField) {
		return ErrValidation(map[string]string{searchField.Field: searchField.Message})
	}
	var notifyField *notify.FieldError
	if errors.As(err, &notifyField) {
		return ErrValidation(map[string]string{notifyField.Field: notifyField.Message})
	}
	var commentField *comment.FieldError
	if errors.As(err, &commentField) {
		return ErrValidation(map[string]string{commentField.Field: sentence(commentField.Message)})
	}
	var reactionField *reaction.FieldError
	if errors.As(err, &reactionField) {
		return ErrValidation(map[string]string{reactionField.Field: sentence(reactionField.Message)})
	}
	var webhookField *webhook.FieldError
	if errors.As(err, &webhookField) {
		return ErrValidation(map[string]string{webhookField.Field: webhookField.Message})
	}
	var shareField *share.FieldError
	if errors.As(err, &shareField) {
		return ErrValidation(map[string]string{shareField.Field: shareField.Message})
	}
	if errors.Is(err, unfurl.ErrBadURL) {
		return ErrValidation(map[string]string{"url": "Give the full address of a web page, starting with https:// or http://."})
	}
	var hubField *hub.FieldError
	if errors.As(err, &hubField) {
		return ErrValidation(map[string]string{hubField.Field: hubField.Message})
	}
	if errors.Is(err, hub.ErrNotAdmin) {
		return ErrForbidden("Only an administrator of the organization chooses its hub. Ask one of them to change it.")
	}
	var shortcutField *shortcut.FieldError
	if errors.As(err, &shortcutField) {
		return ErrValidation(map[string]string{shortcutField.Field: shortcutField.Message})
	}
	var full *shortcut.FullError
	if errors.As(err, &full) {
		return ErrConflict(full.Error())
	}
	var taken *space.PersonalTakenError
	if errors.As(err, &taken) {
		return ErrConflict(taken.Error())
	}
	var closed *share.CannotViewError
	if errors.As(err, &closed) {
		return &APIError{Status: http.StatusConflict, Code: "cannot_view", Message: closed.Error()}
	}
	var braked *share.RateLimitedError
	if errors.As(err, &braked) {
		return &APIError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: braked.Error()}
	}
	var armatureField *armature.FieldError
	if errors.As(err, &armatureField) {
		return ErrValidation(map[string]string{armatureField.Field: armatureField.Message})
	}
	var armatureRefused *armature.RefusedError
	if errors.As(err, &armatureRefused) {
		return &APIError{Status: armatureRefused.Status, Code: armatureRefused.Code, Message: sentence(armatureRefused.Message),
			Fields: armatureRefused.Fields, Position: armatureRefused.Position, cause: err}
	}
	var denied *perm.DeniedError
	if errors.As(err, &denied) && denied.Archived != perm.NotArchived {
		return &APIError{Status: http.StatusConflict, Code: "archived", Message: denied.Error()}
	}
	if errors.As(err, &denied) {
		return ErrForbidden(denied.Error())
	}
	var archivedWith *page.ArchivedWithError
	if errors.As(err, &archivedWith) {
		return &APIError{Status: http.StatusConflict, Code: "archived", Message: sentence(archivedWith.Error())}
	}
	var badUpload *mdio.InvalidError
	if errors.As(err, &badUpload) {
		msg := sentence(badUpload.Message)
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: msg, Fields: map[string]string{"file": msg}}
	}
	var bigUpload *mdio.TooLargeError
	if errors.As(err, &bigUpload) {
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: sentence(bigUpload.Error())}
	}
	var badDoc *document.InvalidError
	if errors.As(err, &badDoc) {
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: sentence(badDoc.Message)}
	}
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		return ErrUnauthorized("Your session has expired. Sign in again.")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_credentials",
			Message: "That email and password do not match an account. Check both and try again, or sign in through your organization's provider."}
	case errors.Is(err, auth.ErrUserInactive):
		return &APIError{Status: http.StatusForbidden, Code: "account_inactive",
			Message: "This account has been deactivated. Ask an administrator of your organization to turn it back on."}
	case errors.Is(err, auth.ErrNotAMember):
		return &APIError{Status: http.StatusForbidden, Code: "not_a_member",
			Message: "You are not a member of that organization. Check its name, or ask one of its administrators to let you in."}
	case errors.Is(err, auth.ErrSessionStaysHome):
		return &APIError{Status: http.StatusForbidden, Code: "session_stays_home",
			Message: "Your sign-in does not reach that organization. Sign in there through its own sign-in page."}
	case errors.Is(err, auth.ErrBadLocale):
		return ErrValidation(map[string]string{"locale": "Choose English, German, or the language of your browser."})
	case errors.Is(err, auth.ErrNoSuchRequest):
		return ErrNotFound("That person is not waiting to be let in. Reload the list; somebody may have answered already.")
	case errors.Is(err, auth.ErrNoSuchMember):
		return ErrNotFound("That person is not a member here. Reload the list; somebody may have removed them already.")
	case errors.Is(err, auth.ErrOwnerStays):
		return ErrConflict("The owner of an organization cannot be removed. Remove somebody else, or ask the owner.")
	case errors.Is(err, auth.ErrRemoveSelf):
		return ErrConflict("You cannot remove yourself. Ask another administrator to do it.")
	case errors.Is(err, oidc.ErrNoSuchGroupRole):
		return ErrNotFound("That group is not mapped to a role. Reload the list; somebody may have removed it already.")
	case errors.Is(err, auth.ErrBadJoinRole):
		return ErrValidation(map[string]string{"role": "Let the person in as member or admin."})
	case errors.Is(err, auth.ErrTokenName):
		return ErrValidation(map[string]string{"name": sentence(err.Error())})
	case errors.Is(err, auth.ErrTokenScope):
		return ErrValidation(map[string]string{"scopes": sentence(err.Error())})
	case errors.Is(err, auth.ErrTokenExpiry):
		return ErrValidation(map[string]string{"expiresAt": sentence(err.Error())})
	case errors.Is(err, auth.ErrTokenSpaces), errors.Is(err, auth.ErrNoSuchSpace):
		return ErrValidation(map[string]string{"spaces": sentence(err.Error())})
	case errors.Is(err, auth.ErrNoSuchToken):
		return ErrNotFound("That token was not found. It may have been revoked already; reload the list.")
	case errors.Is(err, oidc.ErrNotConfigured):
		return &APIError{Status: http.StatusNotFound, Code: "sso_not_configured",
			Message: "That organization does not sign in through an identity provider. Check its name, or sign in with a password."}
	case errors.Is(err, armature.ErrNotConfigured):
		return &APIError{Status: http.StatusConflict, Code: "armature_not_configured",
			Message: "Your organization has not connected Armature yet. Ask an administrator to connect it under Settings, Armature."}
	case errors.Is(err, armature.ErrNotConnected):
		return &APIError{Status: http.StatusConflict, Code: "armature_not_connected",
			Message: "You have not connected your Armature account. Paste an Armature token under Profile, Armature, then try again."}
	case errors.Is(err, armature.ErrRejected):
		return &APIError{Status: http.StatusConflict, Code: "armature_rejected",
			Message: "Armature no longer accepts your token. Make a new one under Tokens in Armature and paste it under Profile, Armature."}
	case errors.Is(err, armature.ErrUnreachable):
		return &APIError{Status: http.StatusBadGateway, Code: "armature_unreachable",
			Message: "Armature did not answer. Try again in a moment, and tell an administrator if it keeps happening.", cause: err}
	case errors.Is(err, tenant.ErrNoTenant):
		return &APIError{Status: http.StatusBadRequest, Code: "no_organization", Message: "Select an organization first."}
	case errors.Is(err, objectstore.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "storage_unavailable", Message: "Files cannot be stored on this server yet. Ask an administrator to set up file storage.", cause: err}
	case errors.Is(err, theme.ErrNotAThemeFile), errors.Is(err, theme.ErrDefaultNotShared):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: sentence(err.Error())}
	case errors.Is(err, space.ErrNotFound):
		return ErrNotFound("That space was not found. Check the key in the address; the space may have been deleted.")
	case errors.Is(err, page.ErrNotFound), errors.Is(err, watch.ErrPageNotFound), errors.Is(err, comment.ErrPageNotFound), errors.Is(err, reaction.ErrPageNotFound),
		errors.Is(err, share.ErrPageNotFound):
		return ErrNotFound("That page was not found. It may have been moved or deleted; look for it from its space.")
	case errors.Is(err, shortcut.ErrNotFound):
		return ErrNotFound("That shortcut was not found. Somebody may have removed it already; reload the list.")
	case errors.Is(err, comment.ErrNotFound), errors.Is(err, reaction.ErrCommentNotFound):
		return ErrNotFound("That comment was not found. It may have been deleted, or its page moved; reload the page.")
	case errors.Is(err, comment.ErrUnpublished):
		return &APIError{Status: http.StatusConflict, Code: "unpublished",
			Message: "This page has not been published yet, so nobody else can read a comment on it. Publish the page first, then comment."}
	case errors.Is(err, reaction.ErrUnpublished):
		return &APIError{Status: http.StatusConflict, Code: "unpublished",
			Message: "This page has not been published yet, so nobody else can see a reaction to it. Publish the page first, then react."}
	case errors.Is(err, share.ErrUnpublished):
		return &APIError{Status: http.StatusConflict, Code: "unpublished",
			Message: "This page has not been published yet, so nobody else can read it. Publish the page first, then share it."}
	case errors.Is(err, reaction.ErrMayNotReact):
		return ErrForbidden("You may not react in this space. Ask an administrator of the space for access.")
	case errors.Is(err, comment.ErrAnchorConflict):
		return &APIError{Status: http.StatusConflict, Code: "anchor_conflict",
			Message: "The page changed after you selected the passage. Read the page again and select the passage once more."}
	case errors.Is(err, comment.ErrNotInline):
		return &APIError{Status: http.StatusConflict, Code: "not_inline",
			Message: "Only a thread on a passage can be resolved or reopened. Reply to a thread below the page instead."}
	case errors.Is(err, comment.ErrThreadTaken):
		return ErrConflict("That thread id is taken. Pick the passage again, which gives it a new id.")
	case errors.Is(err, comment.ErrNotYours):
		return ErrForbidden("You can only change or delete your own comments. Ask an administrator of the space to delete somebody else's.")
	case errors.Is(err, perm.ErrFixed):
		return ErrConflict("Administering the organization follows the owner and admin roles. Change somebody's role under Users instead.")
	case errors.Is(err, perm.ErrUnknownPermission):
		return ErrNotFound("There is no such global permission. Choose use, createSpace or administer.")
	case errors.Is(err, page.ErrNotStewardable):
		return ErrConflict("Publish the page before you name its owner or verify it.")
	case errors.Is(err, page.ErrLocksOut):
		return ErrConflict("These restrictions would leave you unable to view or edit the page. Add yourself, or a group you are in, to both lists.")
	case errors.Is(err, page.ErrNotInTrash):
		return ErrNotFound("That page is not in this space's trash. Reload the trash; somebody may have restored or deleted it.")
	case errors.Is(err, page.ErrHomeNotArchived), errors.Is(err, page.ErrParentArchived):
		return &APIError{Status: http.StatusConflict, Code: "archived", Message: sentence(err.Error())}
	case errors.Is(err, page.ErrHomeNotTrashed), errors.Is(err, page.ErrCycle), errors.Is(err, page.ErrHomeFixed), errors.Is(err, page.ErrNotASibling):
		return ErrConflict(sentence(err.Error()))
	case errors.Is(err, page.ErrFolder):
		return &APIError{Status: http.StatusConflict, Code: "folder", Message: sentence(err.Error())}
	case errors.Is(err, page.ErrBadKind):
		return ErrValidation(map[string]string{"kind": sentence(err.Error())})
	case errors.Is(err, page.ErrStale):
		return ErrConflict("Somebody else saved this page after you opened it. Copy your changes, reload the page and make them again.")
	case errors.Is(err, attachment.ErrNotFound):
		return ErrNotFound("That file was not found. It may have been deleted, or its page moved to the trash.")
	case errors.Is(err, attachment.ErrTooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: sentence(err.Error())}
	case errors.Is(err, attachment.ErrEmpty):
		return ErrValidation(map[string]string{"file": sentence(attachment.ErrEmpty.Error())})
	case errors.Is(err, objectstore.ErrNoObject):
		return &APIError{Status: http.StatusNotFound, Code: "file_missing", Message: "The file's contents are missing from storage. Upload it again, or ask an administrator to check the file storage.", cause: err}
	case errors.Is(err, page.ErrPublishConflict):
		return &APIError{Status: http.StatusConflict, Code: "publish_conflict",
			Message: "Somebody published this page after you began your draft. Compare the two, then discard your draft or save it again over theirs and publish."}
	case errors.Is(err, page.ErrNoDraft):
		return &APIError{Status: http.StatusConflict, Code: "no_draft",
			Message: "You have no draft of this page to publish. Edit the page first; your changes are saved as a draft."}
	case errors.Is(err, page.ErrDraftNotFound):
		return ErrNotFound("You have no draft of this page. Compare two versions instead, or edit the page to start one.")
	case errors.Is(err, page.ErrVersionNotFound):
		return ErrNotFound("That version of the page was not found. Open the page's history to see the versions it has.")
	case errors.Is(err, page.ErrRestoreStale):
		return ErrConflict("Somebody published this page after you opened its history. Reload the history and restore again.")
	case errors.Is(err, page.ErrRestoreLatest):
		return ErrConflict("That is already the latest version of the page. Pick an older version to restore.")
	case errors.Is(err, mdio.ErrNoMarkdown):
		msg := sentence(err.Error())
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: msg, Fields: map[string]string{"file": msg}}
	case errors.Is(err, mdio.ErrTooManyBelow):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: sentence(err.Error())}
	case errors.Is(err, webhook.ErrNotFound):
		return ErrNotFound("That webhook or delivery is not here. It may have been deleted; reload the list.")
	case errors.Is(err, webhook.ErrNoKey):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "secret_key_missing",
			Message: "Webhooks need STATOR_SECRET_KEY to keep their secrets. Ask the operator to set it, then try again.", cause: err}
	case errors.Is(err, theme.ErrNotFound):
		return ErrNotFound("That theme was not found. It may have been deleted or taken private.")
	case errors.Is(err, theme.ErrAssetNotFound):
		return ErrNotFound("That file is not in the theme.")
	case errors.Is(err, theme.ErrNotYours):
		return ErrForbidden(sentence(theme.ErrNotYours.Error()))
	case errors.Is(err, theme.ErrDuplicateName), errors.Is(err, theme.ErrAssetInUse), errors.Is(err, theme.ErrTooManyAssets):
		return ErrConflict(sentence(err.Error()))
	case errors.Is(err, theme.ErrBadAssetType), errors.Is(err, theme.ErrUnsafeSVG):
		return ErrBadRequest(sentence(err.Error()))
	case errors.Is(err, theme.ErrAssetTooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: sentence(theme.ErrAssetTooLarge.Error())}
	default:
		return ErrInternal(err)
	}
}

// respondError writes err in the envelope and logs the cause when there is one
// worth seeing.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := toAPIError(err)
	body := *apiErr
	body.RequestID = RequestIDFrom(r.Context())

	log := loggerFrom(r.Context())
	if body.Status >= http.StatusInternalServerError {
		log.Error("request failed", "code", body.Code, "error", err)
	} else {
		log.Debug("request rejected", "code", body.Code, "status", body.Status, "message", body.Message)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(body.Status)
	if encErr := json.NewEncoder(w).Encode(errorEnvelope{Error: body}); encErr != nil {
		log.Error("failed to write error response", "error", encErr)
	}
}

// respondJSON writes a successful response.
func respondJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so there is nothing to do but note it.
		loggerFrom(r.Context()).Error("failed to write response body", "error", err)
	}
}
