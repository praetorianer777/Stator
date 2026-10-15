package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// stubVersion is the build the stub names at /meta.
const stubVersion = "0.0.0+stub"

// metaPrefix is where a suite tells the stub how /meta answers by host. It
// is apart from the /_stub/{tenant} controls, since /meta knows no tenant.
const metaPrefix = "/_stub-meta"

// armatureCapabilities are what Armature reports at /meta at the commit
// api/armature/SOURCE pins (backend/internal/httpapi/capabilities.go there);
// the real-Armature suite checks Stator reads the same from Armature itself.
var armatureCapabilities = []string{
	"calendar@1",
	"issue-embed@1",
	"plan@1",
	"remote-link-relations@1",
	"remote-links@1",
	"reports@1",
	"theme-export@1",
	"webhooks@1",
}

// metaOverride makes the stub, reached by one host, answer /meta as an
// older Armature: with fewer capabilities, or with none and no /meta at all.
// /meta takes no token, so the host is what tells one suite's Armature from
// another's.
type metaOverride struct {
	Absent       bool     `json:"absent"`
	Capabilities []string `json:"capabilities"`
}

type metas struct {
	mu     sync.Mutex
	byHost map[string]metaOverride
}

func (m *metas) of(host string) (metaOverride, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.byHost[strings.ToLower(host)]
	return o, ok
}

func (s *stub) meta(c *call) {
	capabilities := armatureCapabilities
	if o, ok := s.metas.of(c.r.Host); ok {
		if o.Absent {
			refuse(c.w, http.StatusNotFound, "not_found", "This Armature is older than /meta.")
			return
		}
		capabilities = o.Capabilities
	}
	if capabilities == nil {
		capabilities = []string{}
	}
	respond(c.w, http.StatusOK, map[string]any{
		"product":      "armature",
		"build":        map[string]any{"version": stubVersion},
		"apiVersion":   "1",
		"capabilities": capabilities,
	})
}

// setMeta is PUT /_stub-meta/{host}: what /meta answers when reached by host.
func (s *stub) setMeta(w http.ResponseWriter, r *http.Request) {
	var o metaOverride
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&o); err != nil {
		refuse(w, http.StatusBadRequest, "bad_request", "Send the capabilities to report, or absent true for an Armature without /meta.")
		return
	}
	s.metas.mu.Lock()
	s.metas.byHost[strings.ToLower(r.PathValue("host"))] = o
	s.metas.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// forgetMeta is DELETE /_stub-meta/{host}: Armature's own capabilities again.
func (s *stub) forgetMeta(w http.ResponseWriter, r *http.Request) {
	s.metas.mu.Lock()
	delete(s.metas.byHost, strings.ToLower(r.PathValue("host")))
	s.metas.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
