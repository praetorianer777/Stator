package armature

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// PageLink is the page a selection was taken from, as an issue's description
// names it.
type PageLink struct {
	ID       uuid.UUID
	SpaceKey string
	Title    string
}

// PageURL is where an issue's description sends people back to a page: no
// slug, so a rename keeps the address.
func PageURL(appURL, spaceKey string, pageID uuid.UUID) string {
	return strings.TrimSuffix(appURL, "/") + "/s/" + spaceKey + "/p/" + pageID.String()
}

// Summary is raw with every run of white space made one space, and whether
// Armature takes it as a summary.
func Summary(raw string) (string, bool) {
	summary := strings.Join(strings.Fields(raw), " ")
	return summary, summary != "" && utf8.RuneCountInString(summary) <= MaxSummaryLength
}

// CheckCreate refuses what Armature need not be asked about, and tidies the
// project key and the summaries in place.
func CheckCreate(in *CreateIssuesInput) error {
	in.ProjectKey = strings.ToUpper(strings.TrimSpace(in.ProjectKey))
	if in.ProjectKey == "" {
		return &FieldError{Field: "projectKey", Message: "Choose the Armature project to file the issues in."}
	}
	if len(in.Items) == 0 || len(in.Items) > MaxCreateItems {
		return &FieldError{Field: "items", Message: fmt.Sprintf("File 1 to %d issues at once, and the rest in another request.", MaxCreateItems)}
	}
	for i := range in.Items {
		summary, ok := Summary(in.Items[i].Summary)
		if !ok {
			return &FieldError{Field: "items", Message: fmt.Sprintf("Give issue %d a summary of 1 to %d characters.", i+1, MaxSummaryLength)}
		}
		in.Items[i].Summary = summary
	}
	return nil
}

// Description is the one paragraph an issue made from a page holds:
// "From {title} in Stator", the title linked to the page.
func Description(title, url string) map[string]any {
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	linked := text(title)
	linked["marks"] = []any{map[string]any{"type": "link", "attrs": map[string]any{"href": url}}}
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{text("From "), linked, text(" in Stator")}},
	}}
}

// CreateRequest is the body Stator sends to Armature's POST /issues.
type CreateRequest struct {
	ProjectKey  string         `json:"projectKey"`
	TypeID      *uuid.UUID     `json:"typeId,omitempty"`
	Summary     string         `json:"summary"`
	Description map[string]any `json:"description"`
}

// CreateIssues files in's items in order as the viewer, stopping at the first
// refusal: the issues made, and the refused item's index with its error.
func (s *Service) CreateIssues(ctx context.Context, in CreateIssuesInput, page PageLink) ([]Issue, int, error) {
	v, status, err := s.Viewer(ctx)
	if err != nil {
		return nil, 0, err
	}
	switch status {
	case StatusNotConfigured:
		return nil, 0, ErrNotConfigured
	case StatusNotConnected:
		return nil, 0, ErrNotConnected
	case StatusRejected:
		return nil, 0, ErrRejected
	}
	description := Description(page.Title, PageURL(s.opts.AppURL, page.SpaceKey, page.ID))
	made := []Issue{}
	for i, item := range in.Items {
		var answer struct {
			Issue Issue `json:"issue"`
		}
		body := CreateRequest{ProjectKey: in.ProjectKey, TypeID: in.TypeID, Summary: item.Summary, Description: description}
		if err := v.Caller.Send(ctx, "POST", "/issues", body, &answer); err != nil {
			if errors.Is(err, ErrRejected) {
				if noteErr := s.NoteRejected(ctx, v); noteErr != nil {
					s.opts.Log.Warn("a refused Armature token could not be marked", "token_id", v.TokenID, "error", noteErr)
				}
			}
			return made, i, err
		}
		issue := answer.Issue
		issue.URL = IssueURL(v.Caller.BaseURL(), issue.Key)
		// The page draws the new chip at once; a null kept for the key from
		// before the issue existed would hide it until it expired.
		s.cache.PutIssue(ctx, v.OrgID, v.TokenID, issue.Key, &issue)
		made = append(made, issue)
	}
	return made, len(in.Items), nil
}

// IssueTypes are Armature's issue types the viewer may file, sub-tasks left
// out, from the same cached entry as Projects.
func (s *Service) IssueTypes(ctx context.Context) (Status, []IssueType, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	m, err := s.meta(ctx, v)
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	return StatusOK, m.IssueTypes, nil
}
