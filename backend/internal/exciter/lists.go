package exciter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// BadQueryCode is what Exciter, and then Stator, answer for a query that does
// not read, with the position where it went wrong.
const BadQueryCode = armature.BadQueryCode

var projectKeyShape = regexp.MustCompile(ProjectKeyPattern)

// Row is a requirement as a list block and the picker show it.
type Row struct {
	ID  uuid.UUID `json:"id"`
	Key string    `json:"key"`
	// URL opens the requirement in Exciter.
	URL        string             `json:"url"`
	ProjectKey string             `json:"projectKey"`
	Title      string             `json:"title"`
	Status     *RequirementStatus `json:"status"`
	Revision   int                `json:"revision"`
	// Trashed is a requirement in Exciter's trash the reader may still find there.
	Trashed bool `json:"trashed"`
}

// ListResult is one window of what a list block's query finds, as the viewer
// may see it; Total is Exciter's count of every match.
type ListResult struct {
	Requirements []Row `json:"requirements"`
	Total        int   `json:"total"`
	Limit        int   `json:"limit"`
	Offset       int   `json:"offset"`
	// Found is false when Exciter has no project by the key the viewer may see.
	Found bool `json:"found"`
}

// Hit is a requirement the picker offers, with the project it is in.
type Hit struct {
	Row
	ProjectName string `json:"projectName"`
}

// Project is an Exciter project a list block may read.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Completion is where the caret stands in a query and what a pick replaces,
// as Exciter's suggest answers it.
type Completion struct {
	// Slot is field, operator, value or keyword.
	Slot   string `json:"slot"`
	Prefix string `json:"prefix"`
	// From and To are the characters a pick replaces, counted from 0.
	From   int      `json:"from"`
	To     int      `json:"to"`
	Quoted bool     `json:"quoted"`
	Words  []string `json:"words"`
	// Field is the field a value is for, and Custom says it is an attribute.
	Field  string `json:"field,omitempty"`
	Custom bool   `json:"custom,omitempty"`
}

// Word is one thing the language takes at the caret: Text is what a pick
// writes, Label what the menu shows, Detail what it is.
type Word struct {
	Text   string `json:"text"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// Suggestions are what may come next at the caret of a list block's query.
type Suggestions struct {
	Completion   Completion `json:"completion"`
	Words        []Word     `json:"words"`
	Requirements []Hit      `json:"requirements"`
}

// NormalizeProject is raw upper case, and whether that is a project key.
func NormalizeProject(raw string) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(raw))
	return key, projectKeyShape.MatchString(key)
}

// CheckQuery refuses a query longer than Exciter reads. A blank one lists
// every requirement of the project.
func CheckQuery(query string) error {
	if utf8.RuneCountInString(query) > MaxQueryLength {
		return &armature.FieldError{Field: "query", Message: "Write a query of at most " + strconv.Itoa(MaxQueryLength) + " characters."}
	}
	return nil
}

func checkProject(raw string) (string, error) {
	key, ok := NormalizeProject(raw)
	if !ok {
		return "", &armature.FieldError{Field: "project", Message: "Name an Exciter project by its key, such as CHK."}
	}
	return key, nil
}

// listsKey is the hash holding every list block's answers on the site; a
// webhook clears all of it, as any change may change what a query finds.
func (s *Service) listsKey(site uuid.UUID) string { return s.cache.Key(site, "lists") }

func listField(tokenID uuid.UUID, project, query string, limit, offset int) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d", project, query, limit, offset))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// gridRow is the part of Exciter's GridRow and RequirementRef a row reads.
type gridRow struct {
	ID         uuid.UUID          `json:"id"`
	Key        string             `json:"key"`
	ProjectKey string             `json:"projectKey"`
	Kind       string             `json:"kind"`
	Title      string             `json:"title"`
	Revision   int                `json:"revision"`
	Status     *RequirementStatus `json:"status"`
	Trashed    bool               `json:"trashed"`
}

func (g gridRow) row(base string) Row {
	return Row{ID: g.ID, Key: g.Key, URL: RequirementURL(base, g.ProjectKey, g.Key), ProjectKey: g.ProjectKey,
		Title: g.Title, Status: g.Status, Revision: g.Revision, Trashed: g.Trashed}
}

type quickHit struct {
	Requirement gridRow `json:"requirement"`
	ProjectName string  `json:"projectName"`
}

// hits are the requirements among found, sections left out: a chip and a
// block name requirements.
func hits(found []quickHit, base string) []Hit {
	out := []Hit{}
	for _, h := range found {
		if h.Requirement.Kind == "section" {
			continue
		}
		out = append(out, Hit{Row: h.Requirement.row(base), ProjectName: h.ProjectName})
	}
	return out
}

// List asks Exciter for a window of the requirements query finds in project,
// as the viewer, so each reader gets the rows they may see and
// currentUser() is them. A query Exciter does not read is a 422 RefusedError
// with Exciter's sentence and position.
func (s *Service) List(ctx context.Context, project, query string, limit, offset int) (Status, *ListResult, error) {
	project, err := checkProject(project)
	if err != nil {
		return "", nil, err
	}
	if err := CheckQuery(query); err != nil {
		return "", nil, err
	}
	v, status, err := s.ViewerFor(ctx, FeatureLists)
	if v == nil {
		return status, nil, err
	}
	field := listField(v.TokenID, project, query, limit, offset)
	var cached ListResult
	if s.cache.Field(ctx, s.listsKey(v.SiteID), field, RequirementCacheTTL, &cached) {
		return StatusOK, &cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	var answer struct {
		Rows  []gridRow `json:"rows"`
		Total int       `json:"total"`
	}
	params := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "kind": {"requirement"}}
	if strings.TrimSpace(query) != "" {
		params.Set("query", query)
	}
	err = v.Caller.Get(ctx, "/projects/"+url.PathEscape(project)+"/requirements", params, &answer)
	result := &ListResult{Requirements: []Row{}, Limit: limit, Offset: offset, Found: true}
	var refused *armature.RefusedError
	switch {
	case errors.As(err, &refused) && refused.Code == BadQueryCode:
		// Exciter's 400 is the request's fault, which Stator answers 422 as
		// it does every input it cannot take.
		return "", nil, &armature.RefusedError{Status: http.StatusUnprocessableEntity, Code: BadQueryCode, Message: refused.Message, Position: refused.Position}
	case errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden):
		result.Found = false
	case err != nil:
		return s.failed(ctx, v, err), nil, nil
	default:
		result.Total = answer.Total
		for _, r := range answer.Rows {
			result.Requirements = append(result.Requirements, r.row(v.Caller.BaseURL()))
		}
	}
	s.cache.PutField(ctx, s.listsKey(v.SiteID), field, RequirementCacheTTL, result)
	return StatusOK, result, nil
}

// CheckWords is the words the picker looks for, with runs of white space
// made one space, or a sentence when there are none or too many.
func CheckWords(words string) (string, error) {
	cleaned := strings.Join(strings.Fields(words), " ")
	if cleaned == "" {
		return "", &armature.FieldError{Field: "q", Message: "Write a word of the requirement's title, or its key, such as CHK-R42."}
	}
	if utf8.RuneCountInString(cleaned) > MaxSearchLength {
		return "", &armature.FieldError{Field: "q", Message: "Write at most " + strconv.Itoa(MaxSearchLength) + " characters to look for."}
	}
	return cleaned, nil
}

// Search is Exciter's quick search as the viewer: the requirements whose key
// or title start with words, at most SearchLimit, for the picker. A key the
// requirement had before is read by key, so a chip may still be made of it.
func (s *Service) Search(ctx context.Context, words string) (Status, []Hit, error) {
	cleaned, err := CheckWords(words)
	if err != nil {
		return "", nil, err
	}
	v, status, err := s.ViewerFor(ctx, FeatureRequirements)
	if v == nil {
		return status, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	var answer struct {
		Requirements []quickHit `json:"requirements"`
	}
	params := url.Values{"q": {cleaned}, "limit": {strconv.Itoa(SearchLimit)}}
	if err := v.Caller.Get(ctx, "/search/quick", params, &answer); err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	found := hits(answer.Requirements, v.Caller.BaseURL())
	key, isKey := NormalizeKey(cleaned)
	if !isKey || slices.ContainsFunc(found, func(h Hit) bool { return h.Key == key }) {
		return StatusOK, found, nil
	}
	status, byKey, err := s.details(ctx, []string{key})
	if status != StatusOK || err != nil {
		return status, nil, err
	}
	if d := byKey[key]; d != nil && !slices.ContainsFunc(found, func(h Hit) bool { return h.Key == d.Key }) {
		r := d.Requirement
		hit := Hit{Row: Row{ID: r.ID, Key: r.Key, URL: r.URL, ProjectKey: r.ProjectKey, Title: r.Title, Status: r.Status,
			Revision: r.Revision, Trashed: r.TrashedAt != nil}, ProjectName: r.ProjectKey}
		found = append([]Hit{hit}, found...)
		if len(found) > SearchLimit {
			found = found[:SearchLimit]
		}
	}
	return StatusOK, found, nil
}

// Projects are the Exciter projects the viewer may view, archived ones left
// out, for a list block's editor.
func (s *Service) Projects(ctx context.Context) (Status, []Project, error) {
	v, status, err := s.ViewerFor(ctx, FeatureProjects)
	if v == nil {
		return status, nil, err
	}
	key := s.cache.Key(v.SiteID, "projects:"+v.TokenID.String())
	var cached []Project
	if s.cache.Field(ctx, key, "all", RequirementCacheTTL, &cached) {
		return StatusOK, cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	var answer struct {
		Projects []Project `json:"projects"`
	}
	if err := v.Caller.Get(ctx, "/projects", nil, &answer); err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	out := answer.Projects
	if out == nil {
		out = []Project{}
	}
	s.cache.PutField(ctx, key, "all", RequirementCacheTTL, out)
	return StatusOK, out, nil
}

// Suggest is what may come next at the caret at of query in project, as
// Exciter's suggest answers the viewer; at counts characters from 0, and a
// negative at is the end of the query.
func (s *Service) Suggest(ctx context.Context, project, query string, at int) (Status, *Suggestions, error) {
	project, err := checkProject(project)
	if err != nil {
		return "", nil, err
	}
	if err := CheckQuery(query); err != nil {
		return "", nil, err
	}
	if at > utf8.RuneCountInString(query) {
		return "", nil, &armature.FieldError{Field: "at", Message: "Put the caret inside the query, at most at its end."}
	}
	v, status, err := s.ViewerFor(ctx, FeatureLists)
	if v == nil {
		return status, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	params := url.Values{"q": {query}, "limit": {strconv.Itoa(SuggestLimit)}}
	if at >= 0 {
		params.Set("at", strconv.Itoa(at))
	}
	var answer struct {
		Completion   Completion `json:"completion"`
		Words        []Word     `json:"words"`
		Requirements []quickHit `json:"requirements"`
	}
	err = v.Caller.Get(ctx, "/projects/"+url.PathEscape(project)+"/requirements/suggest", params, &answer)
	var refused *armature.RefusedError
	switch {
	case errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden):
		return "", nil, &armature.FieldError{Field: "project", Message: "Exciter has no project " + project + " you may view. Pick one of your projects."}
	case errors.As(err, &refused) && refused.Status == http.StatusUnprocessableEntity:
		return "", nil, &armature.FieldError{Field: "at", Message: "Put the caret inside the query, at most at its end."}
	case err != nil:
		return s.failed(ctx, v, err), nil, nil
	}
	out := &Suggestions{Completion: answer.Completion, Words: answer.Words, Requirements: hits(answer.Requirements, v.Caller.BaseURL())}
	if out.Words == nil {
		out.Words = []Word{}
	}
	if out.Completion.Words == nil {
		out.Completion.Words = []string{}
	}
	return StatusOK, out, nil
}
