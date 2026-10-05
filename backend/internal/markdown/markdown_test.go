package markdown

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/document"
)

const (
	fileID  = "0191e7a0-0000-7000-8000-000000000001"
	file2ID = "0191e7a0-0000-7000-8000-000000000002"
	otherID = "0191e7a0-0000-7000-8000-0000000000aa"
	userID  = "0191e7a0-0000-7000-8000-0000000000bb"
)

var testLinks = Links{
	File: func(id string) (string, bool) {
		switch id {
		case fileID:
			return "plan.files/" + PathRef("chart one.png"), true
		case file2ID:
			return "plan.files/notes.txt", true
		}
		return "", false
	},
	Page: func(id string) (string, bool) {
		if id == otherID {
			return "plan/other.md", true
		}
		return "", false
	},
}

func testResolver(dest string) Target {
	switch dest {
	case "plan.files/chart%20one.png":
		return Target{Kind: TargetFile, AttachmentID: fileID, FileName: "chart one.png"}
	case "plan.files/notes.txt":
		return Target{Kind: TargetFile, AttachmentID: file2ID, FileName: "notes.txt"}
	case "plan/other.md", "plan/other.md#part":
		return Target{Kind: TargetPage, Href: "/s/DOCS/p/" + otherID + strings.TrimPrefix(dest, "plan/other.md")}
	}
	return Target{}
}

// canon is a document as JSON values, with null attributes and empty attribute
// maps dropped, so two shapes of the same document compare equal.
func canon(t *testing.T, body []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "attrs" {
					attrs := val.(map[string]any)
					for name, a := range attrs {
						if a == nil {
							delete(attrs, name)
						}
					}
					if len(attrs) == 0 {
						delete(x, k)
						continue
					}
				}
				x[k] = walk(val)
			}
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		}
		return v
	}
	return walk(v)
}

func parseDoc(t *testing.T, body string) document.Node {
	t.Helper()
	if err := document.Validate(json.RawMessage(body)); err != nil {
		t.Fatalf("the test document is not valid: %v\n%s", err, body)
	}
	n, err := document.Parse(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func doc(blocks ...string) string {
	return `{"type":"doc","content":[` + strings.Join(blocks, ",") + `]}`
}

func para(inline ...string) string {
	return `{"type":"paragraph","content":[` + strings.Join(inline, ",") + `]}`
}

func txt(s string, marks ...string) string {
	q, _ := json.Marshal(s)
	if len(marks) == 0 {
		return `{"type":"text","text":` + string(q) + `}`
	}
	return `{"type":"text","text":` + string(q) + `,"marks":[` + strings.Join(marks, ",") + `]}`
}

func roundTrip(t *testing.T, body string) (string, *Result) {
	t.Helper()
	md := Render("Plan", parseDoc(t, body), testLinks)
	got, err := Convert([]byte(md), testResolver)
	if err != nil {
		t.Fatalf("convert:\n%s\n%v", md, err)
	}
	if got.Title != "Plan" {
		t.Errorf("the title came back as %q from\n%s", got.Title, md)
	}
	return md, got
}

func TestEveryNodeComesBackAsItLeft(t *testing.T) {
	cases := map[string]string{
		"marks": doc(para(
			txt("plain "), txt("bold", `{"type":"bold"}`), txt(" and "), txt("italic", `{"type":"italic"}`),
			txt(" "), txt("gone", `{"type":"strike"}`), txt(" "), txt("x := `y`", `{"type":"code"}`), txt(" "),
			txt("both", `{"type":"bold"}`, `{"type":"italic"}`), txt(" "),
			txt("a link", `{"type":"link","attrs":{"href":"https://example.com/a_b?c=(d)","title":"Say \"hi\""}}`),
			txt(" and "), txt("code link", `{"type":"link","attrs":{"href":"/s/DOCS"}}`, `{"type":"code"}`),
		)),
		"headings": doc(
			`{"type":"heading","attrs":{"level":1},"content":[`+txt("One # with hash")+`]}`,
			`{"type":"heading","attrs":{"level":2},"content":[`+txt("Two ")+","+txt("bold", `{"type":"bold"}`)+`]}`,
			`{"type":"heading","attrs":{"level":3},"content":[`+txt("Three")+`]}`,
		),
		"lists": doc(
			`{"type":"bulletList","content":[{"type":"listItem","content":[`+para(txt("one"))+`,{"type":"bulletList","content":[{"type":"listItem","content":[`+para(txt("nested"))+`]}]}]},{"type":"listItem","content":[`+para(txt("two"))+`]}]}`,
			`{"type":"bulletList","content":[{"type":"listItem","content":[`+para(txt("a separate list"))+`]}]}`,
			`{"type":"orderedList","attrs":{"start":3},"content":[{"type":"listItem","content":[`+para(txt("three"))+`,`+para(txt("loose"))+`]},{"type":"listItem","content":[`+para(txt("four"))+`]}]}`,
			`{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true},"content":[`+para(txt("done"))+`]},{"type":"taskItem","attrs":{"checked":false},"content":[`+para(txt("to do"))+`]}]}`,
		),
		"quotes and code": doc(
			`{"type":"blockquote","content":[`+para(txt("quoted"))+`,`+para(txt("twice"))+`]}`,
			`{"type":"codeBlock","attrs":{"language":"go"},"content":[`+txt("func main() {\n\tfmt.Println(\"```\")\n}")+`]}`,
			`{"type":"codeBlock","attrs":{"language":null}}`,
			`{"type":"horizontalRule"}`,
		),
		"table": doc(`{"type":"table","content":[` +
			`{"type":"tableRow","content":[{"type":"tableHeader","attrs":{"colspan":1,"rowspan":1,"colwidth":null,"align":"left"},"content":[` + para(txt("Name")) + `]},{"type":"tableHeader","attrs":{"colspan":1,"rowspan":1,"colwidth":null,"align":"right"},"content":[` + para(txt("Count")) + `]}]},` +
			`{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colspan":1,"rowspan":1,"colwidth":null,"align":"left"},"content":[` + para(txt("a | b"), `{"type":"hardBreak"}`, txt("c")) + `]},{"type":"tableCell","attrs":{"colspan":1,"rowspan":1,"colwidth":null,"align":"right"},"content":[` + para(txt("2", `{"type":"bold"}`)) + `]}]}` +
			`]}`),
		"panels": doc(
			`{"type":"panel","attrs":{"kind":"info"},"content":[`+para(txt("info"))+`]}`,
			`{"type":"panel","attrs":{"kind":"note"},"content":[`+para(txt("note"))+`]}`,
			`{"type":"panel","attrs":{"kind":"success"},"content":[`+para(txt("success"))+`,`+para(txt("more"))+`]}`,
			`{"type":"panel","attrs":{"kind":"warning"},"content":[{"type":"paragraph"}]}`,
			`{"type":"panel","attrs":{"kind":"error"},"content":[`+para(txt("error"))+`]}`,
		),
		"expand": doc(
			`{"type":"expand","attrs":{"title":"Details <& more>"},"content":[`+para(txt("inside"))+`,{"type":"expand","attrs":{"title":""},"content":[`+para(txt("deeper"))+`]}]}`,
			para(txt("after")),
		),
		"files": doc(
			`{"type":"image","attrs":{"attachmentId":"`+fileID+`","alt":"A chart","width":null}}`,
			`{"type":"image","attrs":{"attachmentId":"`+fileID+`","alt":"Sized","width":320}}`,
			para(txt("See "), `{"type":"attachment","attrs":{"attachmentId":"`+file2ID+`","fileName":"notes.txt"}}`, txt(" or "),
				txt("the notes", `{"type":"link","attrs":{"href":"/api/v1/attachments/`+file2ID+`"}}`)),
		),
		"generated and Armature": doc(
			para(`{"type":"mention","attrs":{"id":"`+userID+`","label":"Ada *L*","mentionSuggestionChar":"@"}}`, txt(" is "),
				`{"type":"status","attrs":{"label":"IN PROGRESS","color":"warning"},"marks":[{"type":"bold"}]}`, txt(" until "),
				`{"type":"date","attrs":{"date":"2026-10-01"}}`, txt(" on "), `{"type":"armatureIssue","attrs":{"key":"STA-12"}}`),
			`{"type":"armatureIssueBlock","attrs":{"key":"STA-7"}}`,
			`{"type":"armatureIssueList","attrs":{"query":"project = STA AND text ~ \"<b>\"","columns":["key","summary","status"],"limit":25}}`,
			`{"type":"armatureIssueList","attrs":{"query":"assignee = me()","columns":["key"]}}`,
			`{"type":"armatureChart","attrs":{"project":"CP","query":"project = CP AND text ~ \"<b>\"","chart":"createdResolved","groupBy":"statusCategory","days":90}}`,
			`{"type":"armatureRoadmap","attrs":{"project":"CP","query":"project = CP ORDER BY key","groupBy":"team"}}`,
			`{"type":"propertiesReport","attrs":{"labels":["release-notes","v1.2"],"space":"DOCS","columns":["Owner","Say \"hi\", <b>"]}}`,
			`{"type":"propertiesReport","attrs":{"labels":["adr"],"space":null,"columns":[]}}`,
			`{"type":"labelledPages","attrs":{"labels":["adr","v1.2"],"match":"any","space":"DOCS","sort":"title","limit":20}}`,
			`{"type":"labelledPages","attrs":{"labels":["adr"],"match":"all","space":null,"sort":"updated","limit":10}}`,
			`{"type":"recentlyUpdated","attrs":{"space":null,"limit":5}}`,
			`{"type":"recentlyUpdated","attrs":{"space":"DOCS","limit":50}}`,
			`{"type":"taskReport","attrs":{"space":"DOCS","assignee":"me","due":"week","state":"open","limit":20}}`,
			`{"type":"taskReport","attrs":{"space":null,"assignee":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","due":"none","state":"all","limit":100}}`,
			`{"type":"taskReport","attrs":{"space":null,"assignee":null,"due":"any","state":"done","limit":1}}`,
			`{"type":"attachmentList"}`,
			`{"type":"calendar","attrs":{"calendarId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a77","project":"CP"}}`,
			`{"type":"calendar","attrs":{"calendarId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a78","project":null}}`,
			`{"type":"tableOfContents","attrs":{"maxLevel":2}}`,
			`{"type":"childPages","attrs":{"scope":"subtree","depth":2,"sort":"title"}}`,
			`{"type":"childPages","attrs":{"scope":"children","depth":null,"sort":"tree"}}`,
		),
		"includes": doc(
			`{"type":"include","attrs":{"pageId":"`+otherID+`","excerptId":null}}`,
			`{"type":"include","attrs":{"pageId":"`+otherID+`","excerptId":"`+fileID+`"}}`,
		),
		"formulas": doc(
			`{"type":"mathBlock","attrs":{"latex":"\\sum_{i=1}^n i = \\frac{n(n+1)}{2}"}}`,
			`{"type":"mathBlock","attrs":{"latex":"a\n`+"```"+`\nb"}}`,
		),
		"diagrams": doc(
			`{"type":"diagram","attrs":{"source":"flowchart LR\n  A[\"Draft <b>\"] --> B[Published]\n\n  B -.-> A"}}`,
			`{"type":"diagram","attrs":{"source":"sequenceDiagram\n  Ada->>Bob: `+"```"+`"}}`,
		),
		"breaks and links to pages": doc(
			para(txt("line one"), `{"type":"hardBreak"}`, txt("line two")),
			para(txt("other page", `{"type":"link","attrs":{"href":"/s/DOCS/p/`+otherID+`#part"}}`)),
		),
		"text that looks like Markdown": doc(
			para(txt(`1. not a list *nor* _this_ [link](x) <b>tag</b> \ back `+"`tick`"+` ~~ &amp; R&D snake_case`)),
			para(txt("# not a heading")),
			para(txt("- not an item")),
			para(txt("> not a quote")),
			para(txt("+ = ! | plain")),
		),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			md, got := roundTrip(t, body)
			if !reflect.DeepEqual(canon(t, []byte(body)), canon(t, got.Body)) {
				t.Errorf("the document changed on its way through\n%s\nwant %s\ngot  %s", md, body, got.Body)
			}
			if len(got.Warnings) > 0 {
				t.Errorf("a round trip warned: %v\n%s", got.Warnings, md)
			}
		})
	}
}

func TestExportReadsAsMarkdown(t *testing.T) {
	body := doc(
		`{"type":"heading","attrs":{"level":1},"content":[`+txt("Intro")+`]}`,
		`{"type":"panel","attrs":{"kind":"warning"},"content":[`+para(txt("Careful"))+`]}`,
		`{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true},"content":[`+para(txt("done"))+`]}]}`,
		`{"type":"image","attrs":{"attachmentId":"`+fileID+`","alt":"A chart","width":null}}`,
		`{"type":"expand","attrs":{"title":"More"},"content":[`+para(txt("inside"))+`]}`,
		para(`{"type":"status","attrs":{"label":"DONE","color":"success"}}`),
		`{"type":"linkCard","attrs":{"url":"https://example.test/a?b=<c>","view":"embed"}}`,
		para(txt("Energy "), `{"type":"mathInline","attrs":{"latex":"E =\n mc^2 \\$ $"}}`),
		`{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colspan":1,"rowspan":1,"colwidth":null},"content":[`+para(`{"type":"mathInline","attrs":{"latex":"\\|x\\| = |y|"}}`)+`]}]}]}`,
	)
	md := Render("Plan", parseDoc(t, body), testLinks)
	for _, want := range []string{
		`Energy $E = mc^2 \$ \$$`,
		`| $\\|x\\| = \|y\|$ |`,
		"\n\n<https://example.test/a?b=%3Cc%3E>\n\n",
		"# Plan\n\n## Intro\n",
		"> [!WARNING]\n> Careful\n",
		"- [x] done\n",
		"![A chart](plan.files/chart%20one.png)\n",
		"<details>\n<summary>More</summary>\n\ninside\n\n</details>\n",
		`<span data-stator="status" data-color="success">DONE</span>`,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the export lacks %q:\n%s", want, md)
		}
	}
}

func TestWhatMarkdownCannotCarryIsReadable(t *testing.T) {
	body := doc(
		`{"type":"orderedList","attrs":{"start":1,"type":"a"},"content":[{"type":"listItem","content":[`+para(txt("lettered"))+`]}]}`,
		`{"type":"table","content":[`+
			`{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colspan":2,"rowspan":1,"background":"accent"},"content":[`+para(txt("wide"))+`]}]},`+
			`{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colspan":1,"rowspan":1},"content":[`+para(txt("a"))+`,{"type":"bulletList","content":[{"type":"listItem","content":[`+para(txt("item"))+`]}]}]},{"type":"tableCell","attrs":{"colspan":1,"rowspan":1},"content":[`+para(txt("b"))+`]}]}`+
			`]}`,
		para(txt("hinted", `{"type":"hint"}`), txt(" "), txt("discussed", `{"type":"inlineComment","attrs":{"threadId":"`+userID+`"}}`)),
		`{"type":"image","attrs":{"attachmentId":"`+otherID+`","alt":"Somewhere else","width":null}}`,
		`{"type":"columns","content":[{"type":"column","attrs":{"width":67},"content":[`+para(txt("left column"))+`]},{"type":"column","attrs":{"width":33},"content":[{"type":"paragraph"}]},{"type":"column","attrs":{"width":null},"content":[`+para(txt("right column"))+`]}]}`,
		`{"type":"decision","attrs":{"state":"decided"},"content":[`+txt("Ship on Fridays")+`]}`,
		`{"type":"decision","attrs":{"state":"undecided"},"content":[`+txt("Which region")+`]}`,
		`{"type":"excerpt","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71","name":"Hours"},"content":[`+para(txt("Inside the excerpt"))+`]}`,
		`{"type":"properties","content":[{"type":"propertyRow","attrs":{"key":"Owner"},"content":[`+txt("Ada", `{"type":"bold"}`)+`]},{"type":"propertyRow","attrs":{"key":"Due"}}]}`,
	)
	md := Render("Plan", parseDoc(t, body), testLinks)
	for _, want := range []string{"1. lettered", "| wide |  |", "| a<br>item | b |", "hinted discussed", "Somewhere else", "left column\n\nright column", "**Decided:** Ship on Fridays", "**Undecided:** Which region", "\n\nInside the excerpt", "| Property | Value |", "| Owner | **Ada** |", "| Due |  |"} {
		if !strings.Contains(md, want) {
			t.Errorf("the export lacks %q:\n%s", want, md)
		}
	}
	got, err := Convert([]byte(md), testResolver)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.Body), `"tableHeader"`) || !strings.Contains(string(got.Body), `"text":"wide"`) {
		t.Errorf("the table did not come back readable: %s", got.Body)
	}
}

func TestTheTitleIsTheOneLeadingHeading(t *testing.T) {
	for _, c := range []struct {
		md, title, firstLevel string
	}{
		{"# Guide\n\n## Setup\n", "Guide", `"level":1`},
		{"Intro first\n\n# Guide\n", "", `"level":1`},
		{"# One\n\n# Two\n", "", `"level":1`},
		{"## Only lower\n\n#### Deep\n\n###### Deepest\n", "", `"level":2`},
	} {
		got, err := Convert([]byte(c.md), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != c.title || !strings.Contains(string(got.Body), c.firstLevel) {
			t.Errorf("%q: title %q, body %s", c.md, got.Title, got.Body)
		}
		if strings.Contains(string(got.Body), `"level":4`) {
			t.Errorf("%q: a heading deeper than a page has: %s", c.md, got.Body)
		}
	}
}

func TestCommonMarkdownComesIn(t *testing.T) {
	md := "Text with <https://example.com> and <ada@example.com>.\n\n" +
		"* star\n+ plus\n\n" +
		"Then code:\n\n    indented code\n\n" +
		"Setext\n======\n\n" +
		"[ref]: https://example.com/ref\n\n[a reference][ref] and ~~gone~~.\n\n" +
		"- [ ] open\n- plain\n\n" +
		"> [!tip]\n> lower case alert\n\n" +
		"<details>\n<summary>Folded</summary>\nall in one block\n</details>\n\n" +
		"![remote](https://example.com/x.png)\n"
	got, err := Convert([]byte(md), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.Body)
	for _, want := range []string{
		`"href":"https://example.com"`, `"href":"mailto:ada@example.com"`, `"type":"codeBlock"`, `"text":"indented code"`,
		`"href":"https://example.com/ref"`, `"type":"strike"`, `"text":"[ ] open"`, `"kind":"success"`,
		`"type":"expand","attrs":{"title":"Folded"}`, `"text":"all in one block"`, `"href":"https://example.com/x.png"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the document lacks %s:\n%s", want, body)
		}
	}
	if len(got.Warnings) == 0 {
		t.Error("a picture from elsewhere did not warn")
	}
}

func TestHostileMarkdownStaysText(t *testing.T) {
	md := "<script>alert(1)</script>\n\n" +
		"Hi <img src=x onerror=alert(1)> <b onclick=alert(1)>there</b> <iframe src=//evil></iframe>\n\n" +
		"[click](javascript:alert(1)) [data](data:text/html,<b>) [proto](//evil.example/x) [back](/\\evil)\n\n" +
		"![pixel](javascript:alert(1))\n\n" +
		`<span data-stator="mention" data-id="not-a-uuid">@Eve</span> ` +
		`<span data-stator="status" data-color="red">X</span> ` +
		`<span data-stator="issue">not a key</span> <span data-stator="date">2026-02-30</span>` + "\n\n" +
		`<div data-stator="issues" data-columns="evil" data-limit="9999">q</div>` + "\n\n" +
		`<div data-stator="toc" onclick="x" data-max-level="1">` + "<script>x</script></div>\n"
	got, err := Convert([]byte(md), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.Body)
	for _, bad := range []string{"javascript", `"href":"data:`, `"href":"//evil`, `"type":"mention"`, `"type":"status"`, `"type":"armatureIssue"`, `"type":"date"`, `"type":"armatureIssueList"`, `"type":"tableOfContents"`, `"type":"image"`} {
		if strings.Contains(body, bad) {
			t.Errorf("hostile Markdown left %s in the document:\n%s", bad, body)
		}
	}
	if !strings.Contains(body, `"language":"html"`) || !strings.Contains(body, `\u003cscript\u003ealert(1)\u003c/script\u003e`) {
		t.Errorf("an HTML block is not shown as code: %s", body)
	}
	for _, kept := range []string{"there", "@Eve", " X ", "click", "not a key"} {
		if !strings.Contains(body, kept) {
			t.Errorf("the words %s were lost: %s", kept, body)
		}
	}
	if err := document.Validate(got.Body); err != nil {
		t.Errorf("the result fails the allowlist: %v", err)
	}
}

// pathologicalBudget is how long the worst input may take, generous for a
// busy machine running the race detector.
const pathologicalBudget = 10 * time.Second

func TestOversizedAndBrokenInputIsRefusedOrRepaired(t *testing.T) {
	if _, err := Convert(make([]byte, MaxSourceBytes+1), nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("an oversized file: %v", err)
	}
	got, err := Convert([]byte("bad \xff\xfe bytes and a \x00 nul"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got.Body) || !strings.Contains(string(got.Body), "\uFFFD") {
		t.Errorf("broken bytes: %s", got.Body)
	}
	empty, err := Convert(nil, nil)
	if err != nil || string(empty.Body) != `{"type":"doc","content":[{"type":"paragraph"}]}` {
		t.Errorf("an empty file: %s %v", empty.Body, err)
	}
}

// nestedByIndent nests list items by indentation alone, one level a line.
func nestedByIndent(levels int) string {
	var b strings.Builder
	for i := range levels {
		b.WriteString(strings.Repeat("  ", i) + "- item\n")
	}
	return b.String()
}

func TestPathologicalMarkdownEndsQuickly(t *testing.T) {
	half := MaxSourceBytes / 2
	for name, src := range map[string]string{
		"quotes":    strings.Repeat(">", half),
		"lists":     strings.Repeat("- ", half/2),
		"brackets":  strings.Repeat("[", half),
		"emphasis":  strings.Repeat("*a", half/2),
		"links":     strings.Repeat("[a](", half/4),
		"backticks": strings.Repeat("`a", half/2),
		"html":      strings.Repeat("<a ", half/3),
		"spans":     strings.Repeat(`<span data-stator="issue">`, half/30),
		"details":   strings.Repeat("<details>\n\n", half/12),
		"indented":  nestedByIndent(1000),
		"mixed":     strings.Repeat("> - 1. ", half/7),
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			got, err := Convert([]byte(src), nil)
			if took := time.Since(start); took > pathologicalBudget {
				t.Errorf("took %v", took)
			}
			if err != nil && !errors.Is(err, ErrTooDeep) && !errors.Is(err, ErrTooTangled) && !errors.Is(err, document.ErrInvalid) {
				t.Errorf("refused for the wrong reason: %v", err)
			}
			if err == nil {
				if verr := document.Validate(got.Body); verr != nil {
					t.Errorf("the result fails the allowlist: %v", verr)
				}
			}
		})
	}
}
