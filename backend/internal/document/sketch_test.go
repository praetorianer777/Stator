package document

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

const sketchBox = `{"id":"box","type":"rectangle","x":0,"y":0,"width":100,"height":50,"angle":0,"strokeColor":"#1e1e1e","backgroundColor":"transparent","link":null}`

func sceneOf(elements ...string) string {
	return `{"elements":[` + strings.Join(elements, ",") + `],"appState":{"viewBackgroundColor":"#ffffff"}}`
}

func sketchDoc(t *testing.T, attrs map[string]any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(map[string]any{"type": "doc", "content": []any{map[string]any{"type": NodeSketch, "attrs": attrs}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// drawingOf is an SVG as Excalidraw exports one, cleaned as the editor cleans it.
func drawingOf(inner string) string {
	return `<svg version="1.1" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 70" width="120" height="70"><defs><style class="style-fonts">
      @font-face { font-family: Excalifont; src: url(data:font/woff2;base64,d09GMgABAAAAA==); }</style></defs>` +
		`<rect x="0" y="0" width="120" height="70" fill="#ffffff"></rect>` + inner + `</svg>`
}

func TestSketchSceneAcceptsWhatTheEditorSaves(t *testing.T) {
	text := `{"id":"t1","type":"text","x":10,"y":10,"width":80,"height":25,"text":"Hello\nthere","originalText":"Hello there","fontSize":20,"fontFamily":5,"containerId":null,"isDeleted":false}`
	arrow := `{"id":"a1","type":"arrow","x":0,"y":0,"width":10,"height":10,"points":[[0,0],[10,10.5]],"endArrowhead":"arrow","link":"https://example.test/plan"}`
	free := `{"id":"f1","type":"freedraw","x":0,"y":0,"width":1,"height":1,"points":[[0,0],[1,1]],"pressures":[0.5,0.5],"strokeColor":"rgb(10, 20, 30)"}`
	for _, scene := range []string{
		sceneOf(),
		`{"elements":[]}`,
		sceneOf(sketchBox, text, arrow, free),
		sceneOf(`{"id":"frame-1","type":"frame","x":0,"y":0,"width":10,"height":10,"name":"Area"}`),
	} {
		if err := ValidateSketchScene(scene); err != nil {
			t.Errorf("%s: %v", scene, err)
		}
		if err := Validate(sketchDoc(t, map[string]any{"scene": scene, "drawing": nil, "title": "A plan"})); err != nil {
			t.Errorf("document with %s: %v", scene, err)
		}
	}
}

func TestSketchSceneRefusesInASentence(t *testing.T) {
	many := make([]string, MaxSketchElements+1)
	for i := range many {
		many[i] = `{"id":"e` + strconv.Itoa(i) + `","type":"line","x":0,"y":0}`
	}
	points := make([]string, MaxSketchPoints+1)
	for i := range points {
		points[i] = "[0,0]"
	}
	cases := []struct{ name, scene, want string }{
		{"not JSON", `{"elements":[`, "not a drawing the editor can read"},
		{"two values", `{"elements":[]} {}`, "not a drawing the editor can read"},
		{"a list", `[]`, "not a drawing the editor can read"},
		{"no elements", `{"appState":{}}`, "not a drawing the editor can read"},
		{"elements not a list", `{"elements":{}}`, "not a drawing the editor can read"},
		{"files", `{"elements":[],"files":{}}`, `holds "files"`},
		{"other state", `{"elements":[],"appState":{"zoom":{"value":2}}}`, `editor setting "zoom"`},
		{"background not a colour", `{"elements":[],"appState":{"viewBackgroundColor":"url(https://x.test/a.png)"}}`, "background that is not a colour"},
		{"element not an object", sceneOf(`"box"`), "not a drawing the editor can read"},
		{"picture", sceneOf(`{"id":"i1","type":"image","fileId":"abc","x":0,"y":0}`), "holds a picture"},
		{"file on a box", sceneOf(`{"id":"i1","type":"rectangle","fileId":"abc"}`), "holds a picture"},
		{"embedded site", sceneOf(`{"id":"e1","type":"embeddable","link":"https://example.test"}`), `holds a "\"embeddable\""`},
		{"no type", sceneOf(`{"id":"e1"}`), `holds a "null"`},
		{"no id", sceneOf(`{"type":"rectangle"}`), "no id the editor can read"},
		{"odd id", sceneOf(`{"id":"a b","type":"rectangle"}`), "no id the editor can read"},
		{"twice the same id", sceneOf(sketchBox, sketchBox), `share the id "box"`},
		{"script link", sceneOf(`{"id":"l1","type":"rectangle","link":"javascript:alert(1)"}`), "not a web or mail address"},
		{"link not text", sceneOf(`{"id":"l1","type":"rectangle","link":7}`), "not a web or mail address"},
		{"custom data", sceneOf(`{"id":"c1","type":"rectangle","customData":{"a":1}}`), "data of another program"},
		{"colour not a colour", sceneOf(`{"id":"c1","type":"rectangle","strokeColor":"url(#x)"}`), "is not one"},
		{"position not a number", sceneOf(`{"id":"p1","type":"rectangle","x":"10"}`), "x that is not a number"},
		{"points not pairs", sceneOf(`{"id":"p1","type":"line","points":[[0,0,0]]}`), "points the editor cannot read"},
		{"points not numbers", sceneOf(`{"id":"p1","type":"line","points":[["0",0]]}`), "points the editor cannot read"},
		{"too many points", sceneOf(`{"id":"p1","type":"line","points":[` + strings.Join(points, ",") + `]}`), "more points than"},
		{"text without words", sceneOf(`{"id":"t1","type":"text"}`), "no words the editor can read"},
		{"original text not words", sceneOf(`{"id":"t1","type":"text","text":"a","originalText":5}`), "no words the editor can read"},
		{"deleted not a yes or no", sceneOf(`{"id":"t1","type":"rectangle","isDeleted":"yes"}`), "neither kept nor deleted"},
		{"too many shapes", sceneOf(many...), "keep it to 5000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSketchScene(c.scene)
			if err == nil {
				t.Fatalf("accepted %.80s", c.scene)
			}
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %q, want it to say %q", err, c.want)
			}
			if !strings.HasSuffix(err.Error(), ".") {
				t.Errorf("%q is no sentence", err)
			}
		})
	}
}

func TestSketchSizeLimits(t *testing.T) {
	long := `{"elements":[{"id":"t","type":"text","text":"` + strings.Repeat("x", MaxSketchSceneLength) + `"}]}`
	err := Validate(sketchDoc(t, map[string]any{"scene": long, "drawing": nil, "title": nil}))
	if err == nil || !strings.Contains(err.Error(), `attribute scene=`) {
		t.Errorf("a scene past its limit: %v", err)
	}
	drawing := drawingOf(`<text>` + strings.Repeat("x", MaxSketchDrawingLength) + `</text>`)
	err = Validate(sketchDoc(t, map[string]any{"scene": sceneOf(), "drawing": drawing, "title": nil}))
	if err == nil || !strings.Contains(err.Error(), `attribute drawing=`) {
		t.Errorf("a drawing past its limit: %v", err)
	}
	err = Validate(sketchDoc(t, map[string]any{"scene": sceneOf(), "drawing": nil, "title": strings.Repeat("x", MaxAltLength+1)}))
	if err == nil || !strings.Contains(err.Error(), `attribute title=`) {
		t.Errorf("a title past its limit: %v", err)
	}
	err = Validate(sketchDoc(t, map[string]any{"scene": " ", "drawing": nil, "title": nil}))
	if err == nil {
		t.Error("an empty scene was accepted")
	}
	err = Validate(sketchDoc(t, map[string]any{"scene": sceneOf(), "drawing": nil, "title": nil, "svg": "<svg/>"}))
	if err == nil || !strings.Contains(err.Error(), `attribute "svg"`) {
		t.Errorf("an attribute of its own: %v", err)
	}
}

func TestSketchDrawingAcceptsWhatExcalidrawDraws(t *testing.T) {
	for _, inner := range []string{
		``,
		`<g stroke-linecap="round" transform="translate(10 10) rotate(0 50 25)"><path d="M0 0 C 10 10, 20 20, 30 30" stroke="#1e1e1e" stroke-width="2" fill="none"></path></g>`,
		`<g transform="translate(10 10) rotate(0 40 12.5)"><text x="40" y="17.6" font-family="Excalifont, Xiaolai, Segoe UI Emoji" font-size="20px" fill="#1e1e1e" text-anchor="middle" style="white-space: pre;" direction="ltr" dominant-baseline="alphabetic">A &lt;b&gt; tag stays words</text></g>`,
		`<mask id="mask-a1"><rect x="0" y="0" fill="#fff" width="100" height="100" opacity="1"></rect><rect x="10" y="10" fill="#000" width="20" height="20" opacity="1"></rect></mask><g mask="url(#mask-a1)"><path d="M0 0 L10 10"></path></g>`,
		`<defs><clipPath id="frame-1"><rect width="10" height="10" rx="8" ry="8"></rect></clipPath></defs><g clip-path="url(#frame-1)"></g>`,
		`<!-- svg-source:excalidraw -->`,
	} {
		if err := ValidateSketchDrawing(drawingOf(inner)); err != nil {
			t.Errorf("%s: %v", inner, err)
		}
	}
	if err := ValidateSketchDrawing(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`); err != nil {
		t.Errorf("an XML declaration first: %v", err)
	}
	if err := ValidateSketchDrawing(`<svg xmlns="http://www.w3.org/2000/svg"><style class="style-fonts"></style></svg>`); err != nil {
		t.Errorf("an empty style: %v", err)
	}
}

func TestSketchDrawingRefusesWhatCouldRunOrFetch(t *testing.T) {
	cases := []struct{ name, drawing, want string }{
		{"not XML", `<svg xmlns="http://www.w3.org/2000/svg"><g></svg>`, "not well-formed SVG"},
		{"not SVG", `<html><body></body></html>`, `"html" element`},
		{"SVG without its namespace", `<svg></svg>`, `"svg" element`},
		{"two drawings", `<svg xmlns="http://www.w3.org/2000/svg"></svg><svg xmlns="http://www.w3.org/2000/svg"></svg>`, "one svg element"},
		{"nothing", ``, "one svg element"},
		{"a group as the root", `<g xmlns="http://www.w3.org/2000/svg"></g>`, "one svg element"},
		{"text outside", `<svg xmlns="http://www.w3.org/2000/svg"></svg>words`, "text outside"},
		{"script", drawingOf(`<script>alert(1)</script>`), `"script" element`},
		{"script in another namespace", drawingOf(`<script xmlns="http://www.w3.org/1999/xhtml">alert(1)</script>`), `"script" element`},
		{"foreign object", drawingOf(`<foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject>`), `"foreignObject" element`},
		{"picture", drawingOf(`<image href="https://x.test/a.png"></image>`), `"image" element`},
		{"use", drawingOf(`<use href="#a"></use>`), `"use" element`},
		{"link", drawingOf(`<a href="javascript:alert(1)"><rect></rect></a>`), `"a" element`},
		{"animation", drawingOf(`<animate attributeName="href" to="javascript:alert(1)"></animate>`), `"animate" element`},
		{"handler", drawingOf(`<rect onload="alert(1)"></rect>`), `attribute "onload"`},
		{"handler on the root", `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`, `attribute "onload"`},
		{"xlink", drawingOf(`<rect xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="#a"></rect>`), "the attribute"},
		{"another default namespace", `<svg xmlns="http://www.w3.org/1999/xhtml"></svg>`, `"svg" element`},
		{"fill from elsewhere", drawingOf(`<rect fill="url(https://x.test/p.svg#a)"></rect>`), `fill=`},
		{"fill from elsewhere in capitals", drawingOf(`<rect fill="URL(https://x.test/p.svg#a)"></rect>`), `fill=`},
		{"style fetching", drawingOf(`<rect style="background: url(https://x.test/a.png)"></rect>`), `style=`},
		{"style escaping", drawingOf(`<rect style="background: u\72l(https://x.test/a.png)"></rect>`), `style=`},
		{"style importing", drawingOf(`<rect style="@import 'https://x.test/a.css'"></rect>`), `style=`},
		{"script in a value", drawingOf(`<rect class="javascript:alert(1)"></rect>`), `class=`},
		{"data in a value", drawingOf(`<rect mask="url(data:image/svg+xml,abc)"></rect>`), `mask=`},
		{"style sheet with rules", drawingOf(`<style>rect { fill: red }</style>`), "a style other than fonts"},
		{"style sheet fetching a font", `<svg xmlns="http://www.w3.org/2000/svg"><style>@font-face { font-family: Excalifont; src: url(https://esm.sh/font.woff2); }</style></svg>`, "a style other than fonts"},
		{"style sheet importing", `<svg xmlns="http://www.w3.org/2000/svg"><style>@import url(https://x.test/a.css);</style></svg>`, "a style other than fonts"},
		{"style sheet with a font and a rule", `<svg xmlns="http://www.w3.org/2000/svg"><style>@font-face { font-family: A; src: url(data:font/woff2;base64,AA==); } svg { background: url(https://x.test/a.png) }</style></svg>`, "a style other than fonts"},
		{"words in a group", drawingOf(`<g>words</g>`), `text inside a "g" element`},
		{"document type", `<!DOCTYPE svg [<!ENTITY a "b">]><svg xmlns="http://www.w3.org/2000/svg"></svg>`, "a document type"},
		{"processing instruction", drawingOf(`<?xml-stylesheet href="https://x.test/a.css"?>`), "a processing instruction"},
		{"unknown entity", drawingOf(`<text>&nbsp;</text>`), "not well-formed SVG"},
		{"nested too deeply", drawingOf(strings.Repeat("<g>", 40) + strings.Repeat("</g>", 40)), "nested too deeply"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSketchDrawing(c.drawing)
			if err == nil {
				t.Fatalf("accepted %.120s", c.drawing)
			}
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %q, want it to say %q", err, c.want)
			}
		})
	}
	err := Validate(sketchDoc(t, map[string]any{"scene": sceneOf(), "drawing": drawingOf(`<script>alert(1)</script>`), "title": nil}))
	if err == nil || !strings.Contains(err.Error(), "open the sketch in the editor and save it again") {
		t.Errorf("a page with a drawing that runs: %v", err)
	}
}

func TestSketchFileRoundTrips(t *testing.T) {
	scene := sceneOf(sketchBox, `{"id":"t1","type":"text","text":"<Hi> & bye","originalText":"<Hi> & bye"}`)
	file, ok := SketchFile(scene)
	if !ok || !strings.Contains(file, `"type": "excalidraw"`) || !strings.Contains(file, `"files": {}`) {
		t.Fatalf("SketchFile = %q, %v", file, ok)
	}
	back, ok := SketchFromFile(file)
	if !ok {
		t.Fatal("the file did not come back")
	}
	var a, b any
	_ = json.Unmarshal([]byte(scene), &a)
	_ = json.Unmarshal([]byte(back), &b)
	if ja, _ := json.Marshal(a); string(ja) != func() string { jb, _ := json.Marshal(b); return string(jb) }() {
		t.Errorf("came back as %s", back)
	}
	if strings.Contains(back, `\`+"u003c") {
		t.Errorf("the words were escaped: %s", back)
	}

	kept, ok := SketchFromFile(`{"type":"excalidraw","elements":[` + sketchBox + `,{"id":"gone","type":"image","isDeleted":true}],"appState":{"viewBackgroundColor":"#fff","zoom":{"value":2},"gridSize":20},"files":{}}`)
	if !ok || strings.Contains(kept, "gone") || strings.Contains(kept, "zoom") || !strings.Contains(kept, `"viewBackgroundColor":"#fff"`) {
		t.Errorf("SketchFromFile kept %s, %v", kept, ok)
	}
	for _, file := range []string{`not json`, `{"elements":"x"}`, `{"type":"excalidraw"}`, `{"elements":[{"id":"i","type":"image"}]}`} {
		if scene, ok := SketchFromFile(file); ok {
			t.Errorf("%s came back as %s", file, scene)
		}
	}
	if _, ok := SketchFile("not json"); ok {
		t.Error("a scene that is no JSON was written")
	}
}

func TestSketchWordsAreReadForSearch(t *testing.T) {
	scene := sceneOf(sketchBox,
		`{"id":"t1","type":"text","text":"Wrapped\nlabel","originalText":"Wrapped label"}`,
		`{"id":"t2","type":"text","text":"Gone","isDeleted":true}`,
		`{"id":"t3","type":"text","text":"Plain"}`)
	body := sketchDoc(t, map[string]any{"scene": scene, "drawing": nil, "title": "Payment flow"})
	root, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := PlainText(root), "Payment flow\nWrapped label\nPlain"; got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
	if words := SketchWords("not json"); words != nil {
		t.Errorf("SketchWords of no JSON = %v", words)
	}
}

func TestSketchIDIsAnIDOrNothing(t *testing.T) {
	scene := sceneOf(sketchBox)
	for _, id := range []any{nil, "5f0c6a2e-9a8b-4c1d-8e2f-0a1b2c3d4e5f"} {
		if err := Validate(sketchDoc(t, map[string]any{"scene": scene, "drawing": nil, "title": nil, "sketchId": id})); err != nil {
			t.Errorf("sketchId %v: %v", id, err)
		}
	}
	for _, id := range []any{"box", "", 7, "5F0C6A2E-9A8B-4C1D-8E2F-0A1B2C3D4E5F"} {
		if err := Validate(sketchDoc(t, map[string]any{"scene": scene, "drawing": nil, "title": nil, "sketchId": id})); !errors.Is(err, ErrInvalid) {
			t.Errorf("sketchId %v: got %v, want a refusal", id, err)
		}
	}
}
