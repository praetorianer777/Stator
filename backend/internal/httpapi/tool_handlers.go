package httpapi

import (
	"net/http"
)

// ToolProduct names another tool of the installation as its /meta does.
type ToolProduct string

const (
	ToolArmature ToolProduct = "armature"
	ToolExciter  ToolProduct = "exciter"
)

// ToolProducts lists every value, in the order the menu shows them.
var ToolProducts = []ToolProduct{ToolArmature, ToolExciter}

// ConnectedTool is another tool Stator is connected to, at the address
// people open it at.
type ConnectedTool struct {
	Product ToolProduct `json:"product"`
	URL     string      `json:"url"`
}

// The other tool decides what the caller may see there, so every member,
// guests included, learns where it is; a tool out of reach is left out.
func (s *Server) handleConnectedTools(w http.ResponseWriter, r *http.Request) {
	tools := []ConnectedTool{}
	if s.Armature != nil {
		base, err := s.Armature.Address(r.Context())
		if err != nil {
			respondError(w, r, err)
			return
		}
		if base != "" {
			tools = append(tools, ConnectedTool{Product: ToolArmature, URL: base})
		}
	}
	if s.Exciter != nil {
		base, err := s.Exciter.Address(r.Context())
		if err != nil {
			respondError(w, r, err)
			return
		}
		if base != "" {
			tools = append(tools, ConnectedTool{Product: ToolExciter, URL: base})
		}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tools": tools})
}
