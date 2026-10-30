package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// A guard of access refuses in its own sentence, as a conflict naming the
// guard, however deep in a transaction the change ran into it.
func TestAccessGuardsAnswerInTheirOwnSentence(t *testing.T) {
	for _, name := range accessConstraints {
		refusal := &pgconn.PgError{Code: "23514", ConstraintName: name, Message: "Do something else first."}
		got := toAPIError(fmt.Errorf("commit: %w", refusal))
		if got.Status != http.StatusConflict || got.Code != name || got.Message != refusal.Message {
			t.Errorf("%s answers %d %s %q", name, got.Status, got.Code, got.Message)
		}
	}
}
