// Package template holds the documents a new page can start from.
//
// Built-ins are data, not rows: one JSON file per language under builtin/,
// read once at start. An organization's own templates are rows, for every
// space or for one, in the same shape and the same list. Each body is a
// document as the editor stores it, so the allowlist judges it like any page,
// and its hints are text carrying the hint mark, which the database strips
// from whatever is published. A template of the organization's may also hold
// variables, which the server fills in when a page is made from it.
package template

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// DateToken in a title is replaced by the client with the day the page is made.
const DateToken = "{date}"

// Scope says whose a template is.
type Scope string

const (
	ScopeBuiltIn      Scope = "builtIn"
	ScopeOrganization Scope = "organization"
	ScopeSpace        Scope = "space"
)

// Scopes is every scope, the most particular first, as the list is ordered.
var Scopes = []Scope{ScopeSpace, ScopeOrganization, ScopeBuiltIn}

// Template is one document a page can start from.
type Template struct {
	// Key is a built-in's name, or the id of one of the organization's.
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Title is what the new page is called, with DateToken for today; empty
	// leaves the title to the author.
	Title string `json:"title"`
	// Body is the whole document the page starts with.
	Body    json.RawMessage `json:"body"`
	BuiltIn bool            `json:"builtIn"`
	Scope   Scope           `json:"scope"`
	// SpaceKey is the space a space's template belongs to, empty otherwise.
	SpaceKey string `json:"spaceKey"`
	// Variables are what the author fills in when making a page from it.
	Variables []Variable `json:"variables"`
	// CanEdit says the caller may change and delete it; never a built-in.
	CanEdit bool `json:"canEdit"`

	spaceID *uuid.UUID
}

var (
	// ErrUnknown is returned for a key that names no template the caller may
	// read.
	ErrUnknown = errors.New("no such template")
	// ErrBuiltIn refuses changing a template the product ships.
	ErrBuiltIn = errors.New("built-in templates come with the product and cannot be changed; make a template of your own instead")
	// ErrUnknownSpace is returned for a space key the caller cannot see.
	ErrUnknownSpace = errors.New("no such space")
)

//go:embed builtin/*.json
var builtinFiles embed.FS

// Language is the one the built-ins are written in until they are translated.
const Language = "en"

var builtins = sync.OnceValues(func() ([]Template, error) {
	raw, err := builtinFiles.ReadFile("builtin/" + Language + ".json")
	if err != nil {
		return nil, err
	}
	var file struct {
		Templates []Template `json:"templates"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("read the built-in templates: %w", err)
	}
	for i := range file.Templates {
		file.Templates[i].BuiltIn = true
		file.Templates[i].Scope = ScopeBuiltIn
		file.Templates[i].Variables = []Variable{}
	}
	return file.Templates, nil
})

// BuiltIns are the templates shipped with the product, in the order offered.
func BuiltIns() []Template {
	list, err := builtins()
	if err != nil {
		// The file is compiled in and a unit test reads it, so this is a
		// broken build rather than something a request could cause.
		panic(err)
	}
	return list
}

// ByKey finds one template.
func ByKey(key string) (Template, error) {
	for _, t := range BuiltIns() {
		if t.Key == key {
			return t, nil
		}
	}
	return Template{}, ErrUnknown
}
