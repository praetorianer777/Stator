package spaceio

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// renames is how an import's ids and names differ from the archive's: every
// page, file, thread and calendar gets a new id, people and the space's key
// those of the organization it lands in.
type renames struct {
	pages, files, threads, calendars map[uuid.UUID]uuid.UUID
	// people maps a person's id in the archive to one here; a person not
	// found here is left out, and their mentions become words.
	people         map[uuid.UUID]uuid.UUID
	oldKey, newKey string
	// asText counts the mentions that became words.
	asText int
}

var (
	pageLink = regexp.MustCompile(`^/s/([^/?#]+)/p/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(.*)$`)
	fileLink = regexp.MustCompile(`^/api/v1/attachments/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(.*)$`)
)

// rewrite points a document at the ids it has here. Numbers keep their
// spelling, so a document that names nothing renamed comes back as it went.
func (r *renames) rewrite(body json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	if !r.node(root) {
		return body, nil
	}
	return json.Marshal(root)
}

func (r *renames) node(v any) bool {
	n, ok := v.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	typ, _ := n["type"].(string)
	attrs, _ := n["attrs"].(map[string]any)
	if attrs != nil {
		for _, ref := range attachment.ReferenceNodes {
			if typ == ref {
				changed = swap(attrs, "attachmentId", r.files) || changed
			}
		}
		switch typ {
		case document.NodeInclude:
			changed = swap(attrs, "pageId", r.pages) || changed
		case document.NodeCalendar:
			changed = swap(attrs, "calendarId", r.calendars) || changed
		case document.NodeTemplateButton:
			changed = swap(attrs, "parent", r.pages) || changed
		case document.NodeTaskReport:
			changed = swap(attrs, "assignee", r.people) || changed
		}
		if key, ok := attrs["space"].(string); ok && key == r.oldKey && r.newKey != r.oldKey {
			attrs["space"] = r.newKey
			changed = true
		}
	}
	if marks, ok := n["marks"].([]any); ok {
		kept := marks[:0]
		for _, m := range marks {
			mark, keep, markChanged := r.mark(m)
			changed = changed || markChanged
			if keep {
				kept = append(kept, mark)
			}
		}
		if len(kept) == 0 {
			delete(n, "marks")
		} else {
			n["marks"] = kept
		}
	}
	if children, ok := n["content"].([]any); ok {
		for i, child := range children {
			if c, ok := child.(map[string]any); ok && c["type"] == "mention" {
				if words := r.mention(c); words != nil {
					children[i] = words
				}
				changed = true
				continue
			}
			changed = r.node(child) || changed
		}
	}
	return changed
}

// mention points a mention at the person here, or, for somebody not found,
// answers the words it showed in its place, so nobody else is told of it.
func (r *renames) mention(n map[string]any) map[string]any {
	attrs, _ := n["attrs"].(map[string]any)
	id, _ := attrs["id"].(string)
	if old, err := uuid.Parse(id); err == nil {
		if fresh, ok := r.people[old]; ok {
			attrs["id"] = fresh.String()
			return nil
		}
	}
	label, _ := attrs["label"].(string)
	r.asText++
	return map[string]any{"type": "text", "text": "@" + label}
}

// mark renames what a mark points at, and drops a passage's mark whose
// thread did not come along.
func (r *renames) mark(v any) (any, bool, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return v, true, false
	}
	attrs, _ := m["attrs"].(map[string]any)
	switch m["type"] {
	case document.AnchorMark:
		id, _ := attrs["threadId"].(string)
		old, err := uuid.Parse(id)
		fresh, ok := r.threads[old]
		if err != nil || !ok {
			return nil, false, true
		}
		attrs["threadId"] = fresh.String()
		return m, true, true
	case "link":
		href, _ := attrs["href"].(string)
		if out := r.href(href); out != href {
			attrs["href"] = out
			return m, true, true
		}
	}
	return m, true, false
}

// href points a link at a page or file of the archive where it now is.
func (r *renames) href(href string) string {
	if m := pageLink.FindStringSubmatch(href); m != nil {
		key, id := m[1], m[2]
		old, _ := uuid.Parse(id)
		fresh, moved := r.pages[old]
		if key != r.oldKey && !moved {
			return href
		}
		if moved {
			id = fresh.String()
		}
		if key == r.oldKey {
			key = r.newKey
		}
		return "/s/" + key + "/p/" + id + m[3]
	}
	if m := fileLink.FindStringSubmatch(href); m != nil {
		old, _ := uuid.Parse(m[1])
		if fresh, ok := r.files[old]; ok {
			return "/api/v1/attachments/" + fresh.String() + m[2]
		}
	}
	return href
}

// swap renames an id attribute found in ids, leaving any other as it is.
func swap(attrs map[string]any, name string, ids map[uuid.UUID]uuid.UUID) bool {
	text, ok := attrs[name].(string)
	if !ok {
		return false
	}
	old, err := uuid.Parse(text)
	if err != nil {
		return false
	}
	fresh, ok := ids[old]
	if !ok {
		return false
	}
	attrs[name] = fresh.String()
	return true
}
