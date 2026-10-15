package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/version"
)

// ProductName is what /meta calls this tool, as Armature and Exciter name
// themselves on theirs.
const ProductName = "stator"

// APIVersion is info.version of the OpenAPI document. It rises only for a
// change that breaks clients.
const APIVersion = "1"

// metaResponse is the answer Stator, Armature and Exciter share, so a partner,
// an operator or the web client can tell which build of which tool answers.
type metaResponse struct {
	Product    string       `json:"product"`
	Build      version.Info `json:"build"`
	APIVersion string       `json:"apiVersion"`
	// Capabilities are the integration features this build offers, as name@version.
	Capabilities []string `json:"capabilities"`
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, metaResponse{
		Product:      ProductName,
		Build:        version.Current(),
		APIVersion:   APIVersion,
		Capabilities: capabilities(),
	})
}
