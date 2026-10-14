//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/spaceio"
)

// readyExport is one export a test follows: the table the worker finishes it
// in, where it is listed and where its file is.
type readyExport struct {
	what, id, table, list, file string
}

// An export the list reports done downloads on the next request, though that
// request reads from a replica that has not replayed the worker's last write:
// the worker's write is nobody's own, so no pin holds the download to it.
func TestADoneExportDownloadsWhileTheReplicaIsBehind(t *testing.T) {
	h := newHarness(t)
	needReplica(t, h)
	behind := transferAPI(t, h, transfersOn(h.appStore(t), h.openCluster(t, 0, neverHealth)))
	ahead := transferAPI(t, h, transfersOn(h.appStore(t), h.primaryOnly(t)))
	home := h.makeMember(t, "export-ready")
	owner := behind.as(t, home.user)
	lister := ahead.as(t, home.user)
	newTree(t, owner, "RDY", "Ready")

	var exports []readyExport
	for _, format := range []string{"archive", "html"} {
		id := obj(t, want(t, owner.post(t, "/api/v1/spaces/RDY/exports", map[string]any{"format": format}), http.StatusAccepted, "export "+format), "export")["id"].(string)
		exports = append(exports, readyExport{"the space as " + format, id, "space_export", "/api/v1/spaces/RDY/exports", "/api/v1/space-exports/" + id + "/file"})
	}
	id := obj(t, want(t, owner.post(t, "/api/v1/site/exports", nil), http.StatusAccepted, "export the organization"), "export")["id"].(string)
	exports = append(exports, readyExport{"the organization", id, "site_export", "/api/v1/site/exports", "/api/v1/site/exports/" + id + "/file"})

	h.settle(t)
	h.pauseReplay(t)
	finishExports(t, h, exports)

	for _, e := range exports {
		t.Run(e.what, func(t *testing.T) {
			state := ""
			for _, each := range list(t, want(t, lister.get(t, e.list), http.StatusOK, "list the exports"), "exports") {
				if job := each.(map[string]any); job["id"] == e.id {
					state, _ = job["state"].(string)
				}
			}
			if state != "done" {
				t.Fatalf("the list read on the primary says %q, want done", state)
			}
			resp, data := owner.download(t, e.file)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("the export listed as done downloads as %d: %s", resp.StatusCode, data)
			}
		})
	}
}

// transfersOn reads exports and imports through c instead of the suite's cluster.
func transfersOn(store objectstore.Store, c *db.Cluster) func(*httpapi.Server) {
	return func(s *httpapi.Server) { s.SpaceTransfers = spaceio.NewJobs(c, store) }
}

// primaryOnly opens a cluster of the test's own without replicas.
func (h *harness) primaryOnly(t *testing.T) *db.Cluster {
	t.Helper()
	cfg := h.cfg.DB
	cfg.ReplicaURLs = nil
	c, err := db.Open(t.Context(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// finishExports runs the worker's watch until every export is done on the
// primary, whatever the replica has replayed.
func finishExports(t *testing.T, h *harness, exports []readyExport) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	watch := spaceio.NewWatch(h.cluster, h.appStore(t), log, time.Hour, time.Hour)
	deadline := time.Now().Add(transferWait)
	for _, e := range exports {
		for {
			var state string
			if err := h.super.QueryRow(context.Background(), `SELECT state FROM `+e.table+` WHERE id = $1`, e.id).Scan(&state); err != nil {
				t.Fatalf("read the state of %s: %v", e.what, err)
			}
			if state == "done" {
				break
			}
			if state != "queued" && state != "running" {
				t.Fatalf("the export of %s ended %s", e.what, state)
			}
			if time.Now().After(deadline) {
				t.Fatalf("the export of %s did not finish within %s", e.what, transferWait)
			}
			if _, err := watch.Once(context.Background()); err != nil {
				t.Logf("the suite's watch: %v", err)
			}
		}
	}
}
