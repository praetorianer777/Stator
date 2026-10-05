package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/praetorianer777/stator/backend/internal/perm"
)

// SpaceTemplate is a structure a new space can start with: its home page,
// the pages below it with their labels, and who may do what in it.
type SpaceTemplate struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Home is the body of the home page, which keeps the space's name.
	Home  json.RawMessage `json:"home"`
	Pages []SpacePage     `json:"pages"`
	// Permissions is what the template grants everyone; whoever makes the
	// space administers it, as in a blank one.
	Permissions SpacePermissions `json:"permissions"`
	BuiltIn     bool             `json:"builtIn"`
}

// SpacePage is one page a space template makes, published, with the pages
// below it in the order given.
type SpacePage struct {
	Title    string          `json:"title"`
	Body     json.RawMessage `json:"body"`
	Labels   []string        `json:"labels"`
	Children []SpacePage     `json:"children"`
}

// SpacePermissions holds what everyone in the organization may do in the
// space; an empty list keeps it to its administrators.
type SpacePermissions struct {
	Everyone []perm.SpacePermission `json:"everyone"`
}

// ErrUnknownSpace is returned for a key that names no space template.
var ErrUnknownSpace = errors.New("no such space template")

var spaceBuiltins = sync.OnceValues(func() ([]SpaceTemplate, error) {
	raw, err := builtinFiles.ReadFile("builtin/spaces." + Language + ".json")
	if err != nil {
		return nil, err
	}
	var file struct {
		Templates []SpaceTemplate `json:"templates"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("read the built-in space templates: %w", err)
	}
	for i := range file.Templates {
		file.Templates[i].BuiltIn = true
	}
	return file.Templates, nil
})

// SpaceBuiltIns are the space templates shipped with the product, in the
// order offered.
func SpaceBuiltIns() []SpaceTemplate {
	list, err := spaceBuiltins()
	if err != nil {
		// Compiled in and read by a unit test: a broken build, not a request.
		panic(err)
	}
	return list
}

// SpaceByKey finds one space template.
func SpaceByKey(key string) (SpaceTemplate, error) {
	for _, t := range SpaceBuiltIns() {
		if t.Key == key {
			return t, nil
		}
	}
	return SpaceTemplate{}, ErrUnknownSpace
}

// Walk visits every page of the template, parents before their children.
func (t SpaceTemplate) Walk(visit func(p SpacePage)) {
	var walk func(pages []SpacePage)
	walk = func(pages []SpacePage) {
		for _, p := range pages {
			visit(p)
			walk(p.Children)
		}
	}
	walk(t.Pages)
}
