package armature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// BadQueryCode is what Armature, and then Stator, answer for a query it
// cannot read, with the position where it went wrong.
const BadQueryCode = "bad_query"

// SearchResult is one page of the issues a query matches, as the viewer may
// see them; Total is Armature's count of every match.
type SearchResult struct {
	Issues []Issue `json:"issues"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	// URL opens the same query in Armature.
	URL string `json:"url"`
}

// SearchURL is where a query opens in the Armature at baseURL.
func SearchURL(baseURL, query string) string {
	return strings.TrimSuffix(baseURL, "/") + "/search?" + url.Values{"q": {query}}.Encode()
}

// CheckQuery refuses a blank query, and one longer than a list block keeps.
func CheckQuery(query string) error {
	if strings.TrimSpace(query) == "" {
		return &FieldError{Field: "q", Message: "Write a query, such as project = CP AND statusCategory != done."}
	}
	if utf8.RuneCountInString(query) > MaxQueryLength {
		return &FieldError{Field: "q", Message: "Write a query of at most " + strconv.Itoa(MaxQueryLength) + " characters."}
	}
	return nil
}

// Search asks Armature for the issues query matches, as the viewer, so each
// reader gets the rows they may see. A query Armature cannot read is a 422
// RefusedError with Armature's sentence and position.
func (s *Service) Search(ctx context.Context, query string, limit, offset int) (Status, *SearchResult, error) {
	if err := CheckQuery(query); err != nil {
		return "", nil, err
	}
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	var cached SearchResult
	if s.cache.Search(ctx, v.OrgID, v.TokenID, query, limit, offset, &cached) {
		return StatusOK, &cached, nil
	}
	var answer struct {
		Issues []Issue `json:"issues"`
		Total  int     `json:"total"`
	}
	params := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	err = v.Caller.Get(ctx, "/issues", params, &answer)
	var refused *RefusedError
	if errors.As(err, &refused) && refused.Code == BadQueryCode {
		// Armature's 400 is the request's fault, which Stator answers 422 as
		// it does every input it cannot take.
		return "", nil, &RefusedError{Status: http.StatusUnprocessableEntity, Code: BadQueryCode, Message: refused.Message, Position: refused.Position}
	}
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	result := &SearchResult{Issues: answer.Issues, Total: answer.Total, Limit: limit, Offset: offset, URL: SearchURL(v.Caller.BaseURL(), query)}
	if result.Issues == nil {
		result.Issues = []Issue{}
	}
	for i := range result.Issues {
		result.Issues[i].URL = IssueURL(v.Caller.BaseURL(), result.Issues[i].Key)
	}
	s.cache.PutSearch(ctx, v.OrgID, v.TokenID, query, limit, offset, result)
	return StatusOK, result, nil
}
