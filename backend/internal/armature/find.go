package armature

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// FindLimit is the most issues one find answers: a single page of
	// suggestions, never paged further.
	FindLimit = 10
	// MaxFindLength bounds the words one find looks for.
	MaxFindLength = 200
)

// nqlStringEscaper backslashes what NQL's lexer reads inside a double-quoted
// string: the quote that would end it and the backslash that escapes.
var nqlStringEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// QuoteNQL is text as one NQL string literal, whatever it holds, so typed
// words can never become operators, fields or another condition.
func QuoteNQL(text string) string {
	return `"` + nqlStringEscaper.Replace(text) + `"`
}

// FindQuery is the NQL that finds issues by words of their summary or
// description, the most recently updated first.
func FindQuery(words string) string {
	return "text ~ " + QuoteNQL(words) + " ORDER BY updated DESC"
}

// CheckWords is the words a find looks for, with runs of white space made
// one space, or a sentence when there are none or too many.
func CheckWords(words string) (string, error) {
	cleaned := strings.Join(strings.Fields(words), " ")
	if cleaned == "" {
		return "", &FieldError{Field: "words", Message: "Write a word of the issue's summary, or its key, such as CP-12."}
	}
	if utf8.RuneCountInString(cleaned) > MaxFindLength {
		return "", &FieldError{Field: "words", Message: "Write at most " + strconv.Itoa(MaxFindLength) + " characters to look for."}
	}
	return cleaned, nil
}

// Find answers the issues whose summary or description holds words, as the
// viewer may see them, most recently updated first and at most FindLimit.
// Words that are an issue key put that issue first, as a key is what a person
// who knows it types.
func (s *Service) Find(ctx context.Context, words string) (Status, []Issue, error) {
	cleaned, err := CheckWords(words)
	if err != nil {
		return "", nil, err
	}
	out := []Issue{}
	if key, ok := NormalizeKey(cleaned); ok {
		status, issue, err := s.Issue(ctx, key)
		if err != nil || status != StatusOK {
			return status, nil, err
		}
		if issue != nil {
			out = append(out, *issue)
		}
	}
	status, found, err := s.Search(ctx, FindQuery(cleaned), FindLimit, 0)
	if err != nil || status != StatusOK {
		return status, nil, err
	}
	for _, each := range found.Issues {
		if len(out) == FindLimit {
			break
		}
		if len(out) > 0 && out[0].Key == each.Key {
			continue
		}
		out = append(out, each)
	}
	return StatusOK, out, nil
}
