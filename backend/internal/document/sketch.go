package document

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
)

// NodeSketch is a drawing made with Excalidraw: its scene as JSON, the SVG
// drawn from it when it was last saved, and the words that stand for it.
const NodeSketch = "sketch"

const (
	// MaxSketchSceneLength bounds a sketch's scene, room for a whiteboard of
	// boxes and freehand lines while a page of several stays under MaxBytes.
	MaxSketchSceneLength = 500_000
	// MaxSketchDrawingLength bounds the SVG drawn from it, which carries the
	// letters of its fonts it uses.
	MaxSketchDrawingLength = 1_000_000
	// MaxSketchElements bounds how many shapes, lines and words one sketch holds.
	MaxSketchElements = 5_000
	// MaxSketchPoints bounds the points of one line or freehand stroke.
	MaxSketchPoints = 20_000
	// maxSketchDrawingDepth bounds how deeply the drawing's groups nest.
	maxSketchDrawingDepth = 32
)

// SketchElementTypes are the Excalidraw elements a sketch may hold. Pictures,
// embedded sites and AI frames are left out: a picture would be a file
// outside the page's own, and an embedded site would load another host.
var SketchElementTypes = []string{"rectangle", "diamond", "ellipse", "arrow", "line", "freedraw", "text", "frame"}

// sketchSceneKeys and sketchAppStateKeys are the parts of an Excalidraw
// scene a sketch keeps; the rest of its state is the editor's, not the drawing's.
var (
	sketchSceneKeys    = []string{"elements", "appState"}
	sketchAppStateKeys = []string{"viewBackgroundColor"}
)

var (
	sketchIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	sketchColorPattern = regexp.MustCompile(`^(?:#[0-9A-Fa-f]{3,8}|transparent|[A-Za-z]{1,30}|(?:rgb|rgba|hsl|hsla)\([0-9., %]{1,60}\))$`)
)

// DrawingRules is what the SVG of a sketch may hold, as the server checks it
// and api/sketch-drawing-allowlist.json gives it to the editor that cleans it.
type DrawingRules struct {
	Elements []string `json:"elements"`
	// TextElements are the elements that may hold text rather than only space.
	TextElements []string `json:"textElements"`
	Attributes   []string `json:"attributes"`
	// Reference is the one url() an attribute may hold: another element of the drawing.
	Reference string `json:"reference"`
	// Forbidden are the words no attribute value may hold, matched ignoring
	// case once every reference is taken out.
	Forbidden []string `json:"forbidden"`
	// FontFace is a style element's whole text: fonts written into the
	// drawing itself, and nothing else.
	FontFace string `json:"fontFace"`
}

// SketchDrawing is the one table of what a sketch's SVG may hold. A drawing is
// shown as a picture, where no script runs and nothing is fetched; the table
// keeps it to shapes and words even where it is opened as a file.
var SketchDrawing = DrawingRules{
	Elements:     []string{"svg", "g", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "defs", "style", "clipPath", "mask", "title", "desc"},
	TextElements: []string{"text", "tspan", "style", "title", "desc"},
	Attributes: []string{
		"version", "viewBox", "width", "height", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry", "d", "points",
		"fill", "fill-opacity", "fill-rule", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-dasharray",
		"stroke-dashoffset", "stroke-miterlimit", "stroke-opacity", "opacity", "transform", "font-family", "font-size",
		"font-style", "font-weight", "text-anchor", "dominant-baseline", "direction", "dir", "style", "id", "class", "mask",
		"clip-path", "clipPathUnits", "maskUnits", "preserveAspectRatio",
	},
	Reference: `url\(#[A-Za-z0-9_-]{1,100}\)`,
	Forbidden: []string{"url(", "javascript", "data:", "expression", "\\", "@", "<", ">", "&"},
	FontFace:  `^\s*(?:@font-face \{ font-family: [A-Za-z][A-Za-z0-9 ]{0,40}; src: url\(data:font/woff2;base64,[A-Za-z0-9+/]*={0,2}\); \}\s*)*$`,
}

var (
	drawingReference = regexp.MustCompile(SketchDrawing.Reference)
	drawingFontFace  = regexp.MustCompile(SketchDrawing.FontFace)
)

const svgNamespace = "http://www.w3.org/2000/svg"

// JSON is the rules as api/sketch-drawing-allowlist.json holds them.
func (r DrawingRules) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// checkSketch holds a sketch's scene to the shape an Excalidraw scene has and
// its drawing to SketchDrawing; the allowlist has already bounded both.
func checkSketch(attrs map[string]any) error {
	if scene, ok := attrs["scene"].(string); ok {
		if err := ValidateSketchScene(scene); err != nil {
			return err
		}
	}
	if drawing, ok := attrs["drawing"].(string); ok {
		if err := ValidateSketchDrawing(drawing); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSketchScene accepts a scene of the elements a sketch may hold, each
// with an id of its own, and refuses anything else in a sentence.
func ValidateSketchScene(scene string) error {
	unreadable := invalid("A sketch on this page is not a drawing the editor can read; open it in the editor and save it again.")
	dec := json.NewDecoder(strings.NewReader(scene))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil || root == nil {
		return unreadable
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return unreadable
	}
	for key := range root {
		if !slices.Contains(sketchSceneKeys, key) {
			return invalid("A sketch on this page holds %q, which a sketch does not keep; open it in the editor and save it again.", key)
		}
	}
	elements, ok := root["elements"].([]any)
	if !ok {
		return unreadable
	}
	if len(elements) > MaxSketchElements {
		return invalid("A sketch on this page holds %d shapes; keep it to %d, or split it into several sketches.", len(elements), MaxSketchElements)
	}
	if state, present := root["appState"]; present {
		if err := checkSketchAppState(state); err != nil {
			return err
		}
	}
	seen := make(map[string]bool, len(elements))
	for _, raw := range elements {
		el, ok := raw.(map[string]any)
		if !ok {
			return unreadable
		}
		if err := checkSketchElement(el); err != nil {
			return err
		}
		id := el["id"].(string)
		if seen[id] {
			return invalid("Two shapes of a sketch on this page share the id %q; open it in the editor and save it again.", id)
		}
		seen[id] = true
	}
	return nil
}

func checkSketchAppState(raw any) error {
	state, ok := raw.(map[string]any)
	if !ok {
		return invalid("A sketch on this page is not a drawing the editor can read; open it in the editor and save it again.")
	}
	for key, value := range state {
		if !slices.Contains(sketchAppStateKeys, key) {
			return invalid("A sketch on this page keeps the editor setting %q, which a sketch does not keep; open it in the editor and save it again.", key)
		}
		if color, ok := value.(string); !ok || !sketchColorPattern.MatchString(color) {
			return invalid("A sketch on this page has a background that is not a colour; pick its background again in the editor.")
		}
	}
	return nil
}

func checkSketchElement(el map[string]any) error {
	typ, _ := el["type"].(string)
	if typ == "image" {
		return invalid("A sketch on this page holds a picture, which a sketch cannot keep yet; take it out and put the picture on the page beside the sketch.")
	}
	if !slices.Contains(SketchElementTypes, typ) {
		return invalid("A sketch on this page holds a %q, which a sketch cannot keep; take it out of the sketch.", shortJSON(el["type"]))
	}
	id, _ := el["id"].(string)
	if !sketchIDPattern.MatchString(id) {
		return invalid("A shape of a sketch on this page has no id the editor can read; open it in the editor and save it again.")
	}
	if link, present := el["link"]; present && link != nil {
		if href, ok := link.(string); !ok || !SafeHref(href) {
			return invalid("A shape of a sketch on this page links to %s, which is not a web or mail address; change the link in the editor.", shortJSON(link))
		}
	}
	if file, present := el["fileId"]; present && file != nil {
		return invalid("A sketch on this page holds a picture, which a sketch cannot keep yet; take it out and put the picture on the page beside the sketch.")
	}
	if data, present := el["customData"]; present && data != nil {
		return invalid("A shape of a sketch on this page carries data of another program; open it in the editor and save it again.")
	}
	for _, key := range []string{"strokeColor", "backgroundColor"} {
		if value, present := el[key]; present {
			if color, ok := value.(string); !ok || !sketchColorPattern.MatchString(color) {
				return invalid("A shape of a sketch on this page has the colour %s, which is not one; pick its colour again in the editor.", shortJSON(value))
			}
		}
	}
	for _, key := range []string{"x", "y", "width", "height", "angle"} {
		if value, present := el[key]; present {
			if _, ok := value.(json.Number); !ok {
				return invalid("A shape of a sketch on this page has a %s that is not a number; open it in the editor and save it again.", key)
			}
		}
	}
	if raw, present := el["points"]; present {
		points, ok := raw.([]any)
		if !ok || len(points) > MaxSketchPoints {
			return invalid("A line of a sketch on this page has more points than %d, or points the editor cannot read; draw it again with fewer.", MaxSketchPoints)
		}
		for _, p := range points {
			pair, ok := p.([]any)
			if !ok || len(pair) != 2 {
				return invalid("A line of a sketch on this page has points the editor cannot read; draw it again.")
			}
			for _, v := range pair {
				if _, ok := v.(json.Number); !ok {
					return invalid("A line of a sketch on this page has points the editor cannot read; draw it again.")
				}
			}
		}
	}
	if deleted, present := el["isDeleted"]; present {
		if _, ok := deleted.(bool); !ok {
			return invalid("A shape of a sketch on this page is neither kept nor deleted; open it in the editor and save it again.")
		}
	}
	_, hasText := el["text"].(string)
	if typ == "text" && !hasText {
		return invalid("A text of a sketch on this page has no words the editor can read; open it in the editor and save it again.")
	}
	for _, key := range []string{"text", "originalText"} {
		if value, present := el[key]; present && value != nil {
			if _, ok := value.(string); !ok {
				return invalid("A text of a sketch on this page has no words the editor can read; open it in the editor and save it again.")
			}
		}
	}
	return nil
}

// SketchFile writes a scene as an Excalidraw file, which Excalidraw opens and
// a Markdown export carries in a fence; a scene that is no JSON writes none.
func SketchFile(scene string) (string, bool) {
	var parts map[string]json.RawMessage
	if json.Unmarshal([]byte(scene), &parts) != nil || parts["elements"] == nil {
		return "", false
	}
	state := parts["appState"]
	if state == nil {
		state = json.RawMessage("{}")
	}
	compact := `{"type":"excalidraw","version":2,"source":"stator","elements":` + string(parts["elements"]) + `,"appState":` + string(state) + `,"files":{}}`
	var out bytes.Buffer
	if json.Indent(&out, []byte(compact), "", "  ") != nil {
		return "", false
	}
	return out.String(), true
}

// SketchFromFile reads an Excalidraw file back as a scene a sketch keeps:
// its elements that are not deleted and its background. It answers false for
// a file that is no scene, or holds what a sketch may not.
func SketchFromFile(file string) (string, bool) {
	var parts struct {
		Elements []json.RawMessage `json:"elements"`
		AppState map[string]any    `json:"appState"`
	}
	if json.Unmarshal([]byte(file), &parts) != nil || parts.Elements == nil {
		return "", false
	}
	kept := make([]json.RawMessage, 0, len(parts.Elements))
	for _, raw := range parts.Elements {
		var head struct {
			IsDeleted bool `json:"isDeleted"`
		}
		if json.Unmarshal(raw, &head) != nil {
			return "", false
		}
		if !head.IsDeleted {
			kept = append(kept, raw)
		}
	}
	scene := struct {
		Elements []json.RawMessage `json:"elements"`
		AppState map[string]any    `json:"appState"`
	}{Elements: kept, AppState: map[string]any{}}
	for _, key := range sketchAppStateKeys {
		if value, ok := parts.AppState[key]; ok {
			scene.AppState[key] = value
		}
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(scene) != nil {
		return "", false
	}
	text := strings.TrimSuffix(out.String(), "\n")
	if ValidateSketchScene(text) != nil || len([]rune(text)) > MaxSketchSceneLength {
		return "", false
	}
	return text, true
}

// SketchWords are the texts written in a scene, in its order, each as it was
// typed rather than as it wraps in its box; a scene that is no JSON has none.
func SketchWords(scene string) []string {
	var parsed struct {
		Elements []struct {
			Type         string  `json:"type"`
			IsDeleted    bool    `json:"isDeleted"`
			Text         *string `json:"text"`
			OriginalText *string `json:"originalText"`
		} `json:"elements"`
	}
	if json.Unmarshal([]byte(scene), &parsed) != nil {
		return nil
	}
	var out []string
	for _, el := range parsed.Elements {
		if el.Type != "text" || el.IsDeleted {
			continue
		}
		words := el.OriginalText
		if words == nil {
			words = el.Text
		}
		if words != nil && *words != "" {
			out = append(out, *words)
		}
	}
	return out
}

// ValidateSketchDrawing accepts an SVG of the elements and attributes
// SketchDrawing names, with references only within itself and fonts only
// written into it, and refuses anything else in a sentence.
func ValidateSketchDrawing(drawing string) error {
	refuse := func(what string) error {
		return invalid("A sketch on this page is drawn with %s, which a drawing may not hold; open the sketch in the editor and save it again.", what)
	}
	dec := xml.NewDecoder(strings.NewReader(drawing))
	dec.Strict = true
	type open struct {
		name string
		text strings.Builder
	}
	var stack []*open
	roots := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return invalid("A sketch on this page has a drawing that is not well-formed SVG; open the sketch in the editor and save it again.")
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			name := tok.Name.Local
			if tok.Name.Space != svgNamespace || !slices.Contains(SketchDrawing.Elements, name) {
				return refuse("a " + shortJSON(name) + " element")
			}
			if len(stack) == 0 {
				roots++
				if name != "svg" || roots > 1 {
					return refuse("something other than one svg element")
				}
			}
			if len(stack) >= maxSketchDrawingDepth {
				return refuse("groups nested too deeply")
			}
			for _, a := range tok.Attr {
				if err := checkDrawingAttr(a, refuse); err != nil {
					return err
				}
			}
			stack = append(stack, &open{name: name})
		case xml.EndElement:
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.name == "style" && !drawingFontFace.MatchString(top.text.String()) {
				return refuse("a style other than fonts written into it")
			}
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(tok)) != "" {
					return refuse("text outside its svg element")
				}
				continue
			}
			top := stack[len(stack)-1]
			if !slices.Contains(SketchDrawing.TextElements, top.name) {
				if strings.TrimSpace(string(tok)) != "" {
					return refuse("text inside a " + shortJSON(top.name) + " element")
				}
				continue
			}
			top.text.Write(tok)
		case xml.Comment:
		case xml.ProcInst:
			if tok.Target != "xml" || len(stack) > 0 || roots > 0 {
				return refuse("a processing instruction")
			}
		case xml.Directive:
			return refuse("a document type")
		}
	}
	if roots != 1 {
		return refuse("something other than one svg element")
	}
	return nil
}

func checkDrawingAttr(a xml.Attr, refuse func(string) error) error {
	// The SVG namespace declared, by the root as Excalidraw writes it.
	if a.Name.Space == "" && a.Name.Local == "xmlns" {
		if a.Value != svgNamespace {
			return refuse("another namespace")
		}
		return nil
	}
	if a.Name.Space != "" || !slices.Contains(SketchDrawing.Attributes, a.Name.Local) {
		return refuse("the attribute " + shortJSON(attrName(a.Name)))
	}
	value := strings.ToLower(drawingReference.ReplaceAllString(a.Value, ""))
	for _, word := range SketchDrawing.Forbidden {
		if strings.Contains(value, word) {
			return refuse("the attribute " + a.Name.Local + "=" + shortJSON(a.Value))
		}
	}
	return nil
}

func attrName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}
