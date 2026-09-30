package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A pending operation is a promise, not a stub that pretends: a member who
// calls one is told it is not built, in the one error envelope.
func TestEveryPendingRouteAnswersNotImplemented(t *testing.T) {
	router := tokenServer(t).Routes(nil)
	pending := 0
	for _, op := range operations {
		if !op.pending {
			continue
		}
		pending++
		path := APIPrefix + pathParamPattern.ReplaceAllString(op.path, uuid.NewString())
		resp, body := serve(t, router, withBearer(op.method, path, "full", `{}`))
		if resp.StatusCode != http.StatusNotImplemented || errorOf(t, body)["code"] != "not_implemented" {
			t.Errorf("%s %s = %d %v, want 501 not_implemented", op.method, op.path, resp.StatusCode, body)
		}
	}
	if pending == 0 {
		t.Skip("nothing is pending")
	}
}

// Somebody who has not signed in learns nothing about a pending operation
// that a built one would not tell them either; a public one answers them 501.
func TestAPendingRouteStillWantsASignIn(t *testing.T) {
	router := tokenServer(t).Routes(nil)
	for _, op := range operations {
		if !op.pending {
			continue
		}
		path := APIPrefix + pathParamPattern.ReplaceAllString(op.path, uuid.NewString())
		resp, body := serve(t, router, withBearer(op.method, path, "nobody", `{}`))
		switch {
		case op.public && (resp.StatusCode != http.StatusNotImplemented || errorOf(t, body)["code"] != "not_implemented"):
			t.Errorf("public %s %s answered %d to nobody, want 501 not_implemented", op.method, op.path, resp.StatusCode)
		case !op.public && resp.StatusCode != http.StatusUnauthorized:
			t.Errorf("%s %s answered %d to nobody, want 401", op.method, op.path, resp.StatusCode)
		}
	}
}
