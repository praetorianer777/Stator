package document

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const richDoc = `{"type":"doc","content":[
 {"type":"heading","attrs":{"level":1,"id":"plan"},"content":[{"type":"text","text":"Plan"}]},
 {"type":"paragraph","content":[
  {"type":"text","text":"Ask ","marks":[{"type":"bold"},{"type":"italic"}]},
  {"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01","label":"Ada Lovelace","mentionSuggestionChar":"@"}},
  {"type":"text","text":" first","marks":[{"type":"strike"}]},
  {"type":"hardBreak"},
  {"type":"text","text":"x := 1","marks":[{"type":"code"}]},
  {"type":"text","text":"Say more","marks":[{"type":"hint"}]},
  {"type":"text","text":"site","marks":[{"type":"link","attrs":{"href":"https://example.test","target":"_blank","rel":"noopener noreferrer nofollow","class":null,"title":null}}]}]},
 {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]}]},
 {"type":"orderedList","attrs":{"start":3,"type":null},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"three"}]}]}]},
 {"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true},"content":[{"type":"paragraph","content":[{"type":"text","text":"done"}]}]}]},
 {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
 {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1\ny := 2"}]},
 {"type":"horizontalRule"},
 {"type":"image","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","alt":"The plan","width":480}},
 {"type":"image","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c","alt":null,"width":null}},
 {"type":"paragraph","content":[{"type":"text","text":"See "},{"type":"attachment","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d","fileName":"report.pdf"}}]},
 {"type":"paragraph","content":[{"type":"text","text":"Fixed in "},{"type":"armatureIssue","attrs":{"key":"CP-12"}}]},
 {"type":"armatureIssueBlock","attrs":{"key":"CP-7"}},
 {"type":"armatureIssueList","attrs":{"query":"project = CP AND statusCategory != done","columns":["key","summary","due"],"limit":20}},
 {"type":"armatureChart","attrs":{"project":"CP","query":"project = CP","chart":"pie","groupBy":"statusCategory","days":30}},
 {"type":"armatureRoadmap","attrs":{"project":"CP","query":"project = CP","groupBy":"epic"}},
 {"type":"properties","content":[
  {"type":"propertyRow","attrs":{"key":"Owner"},"content":[{"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","label":"Ada"}}]},
  {"type":"propertyRow","attrs":{"key":""},"content":[{"type":"text","text":"Final","marks":[{"type":"bold"}]}]},
  {"type":"propertyRow","attrs":{"key":"Due"}}]},
 {"type":"propertiesReport","attrs":{"labels":["release-notes","übersicht"],"space":null,"columns":[]}},
 {"type":"propertiesReport","attrs":{"labels":["v1.2"],"space":"DOCS","columns":["Owner","Review date"]}},
 {"type":"labelledPages","attrs":{"labels":["adr","v1.2"],"match":"any","space":null,"sort":"title","limit":50}},
 {"type":"recentlyUpdated","attrs":{"space":"DOCS","limit":1}},
 {"type":"taskReport","attrs":{"space":"DOCS","assignee":"me","due":"week","state":"open","limit":20}},
 {"type":"taskReport","attrs":{"space":null,"assignee":"0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","due":"any","state":"all","limit":100}},
 {"type":"taskReport","attrs":{"space":null,"assignee":null,"due":"none","state":"done","limit":1}},
 {"type":"attachmentList"},
 {"type":"calendar","attrs":{"calendarId":"0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a77","project":"CP"}},
 {"type":"calendar","attrs":{"calendarId":"0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a78","project":null}},
 {"type":"table","content":[
  {"type":"tableRow","content":[
   {"type":"tableHeader","attrs":{"colspan":1,"rowspan":1,"colwidth":null,"background":null},"content":[{"type":"paragraph","content":[{"type":"text","text":"Name"}]}]},
   {"type":"tableHeader","attrs":{"colspan":1,"rowspan":1,"colwidth":[120],"background":"accent"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Role"}]}]}]},
  {"type":"tableRow","content":[
   {"type":"tableCell","attrs":{"colspan":2,"rowspan":1,"colwidth":null,"background":"success"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Ada"}]}]}]}]},
 {"type":"panel","attrs":{"kind":"warning"},"content":[
  {"type":"heading","attrs":{"level":2,"id":"careful"},"content":[{"type":"text","text":"Careful"}]},
  {"type":"paragraph","content":[{"type":"text","text":"Hot"}]}]},
 {"type":"expand","attrs":{"title":"Rollback steps"},"content":[
  {"type":"paragraph","content":[{"type":"text","text":"Revert the release"}]},
  {"type":"expand","attrs":{"title":""},"content":[{"type":"paragraph","content":[{"type":"text","text":"Nested detail"}]}]}]},
 {"type":"expand","content":[{"type":"paragraph"}]},
 {"type":"columns","content":[
  {"type":"column","attrs":{"width":33},"content":[{"type":"paragraph","content":[{"type":"text","text":"Left side"}]}]},
  {"type":"column","attrs":{"width":67},"content":[{"type":"paragraph","content":[{"type":"text","text":"Right side"}]},
   {"type":"columns","content":[
    {"type":"column","attrs":{"width":null},"content":[{"type":"paragraph"}]},
    {"type":"column","content":[{"type":"paragraph"}]},
    {"type":"column","content":[{"type":"paragraph"}]}]}]}]},
 {"type":"decision","attrs":{"state":"decided"},"content":[{"type":"text","text":"Ship weekly","marks":[{"type":"bold"}]}]},
 {"type":"decision","attrs":{"state":"undecided"}},
 {"type":"paragraph","content":[{"type":"text","text":"Energy is "},{"type":"mathInline","attrs":{"latex":"E = mc^2"},"marks":[{"type":"bold"}]}]},
 {"type":"mathBlock","attrs":{"latex":"\\int_0^1 x\\,dx = \\frac{1}{2}"}},
 {"type":"diagram","attrs":{"source":"flowchart LR\n  A[Draft] --> B[Published]"}},
 {"type":"linkCard","attrs":{"url":"https://example.test/post","view":"card"}},
 {"type":"include","attrs":{"pageId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80","excerptId":null}},
 {"type":"include","attrs":{"pageId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80","excerptId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a81"}},
 {"type":"excerpt","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70","name":"Support hours"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Nine to five"}]}]},
 {"type":"linkCard","attrs":{"url":"HTTPS://youtu.be/dQw4w9WgXcQ","view":"embed"}},
 {"type":"heading","attrs":{"level":3,"id":null},"content":[{"type":"text","text":"Plan"}]},
 {"type":"tableOfContents","attrs":{"maxLevel":2}},
 {"type":"childPages","attrs":{"scope":"subtree","depth":3,"sort":"updated"}},
 {"type":"panel","attrs":{"kind":"info"},"content":[{"type":"childPages","attrs":{"scope":"children","depth":null,"sort":"title"}},{"type":"tableOfContents"}]}
]}`

func TestValidateAcceptsEveryAllowedConstruct(t *testing.T) {
	if err := Validate(json.RawMessage(richDoc)); err != nil {
		t.Fatalf("a document of every allowed node was refused: %v", err)
	}
	if err := Validate(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)); err != nil {
		t.Fatalf("an empty page was refused: %v", err)
	}
}

func TestValidateRefusesInASentence(t *testing.T) {
	para := func(inner string) string {
		return `{"type":"doc","content":[{"type":"paragraph","content":[` + inner + `]}]}`
	}
	list := func(attrs string) string {
		return `{"type":"doc","content":[{"type":"armatureIssueList","attrs":{` + attrs + `}}]}`
	}
	link := func(href string) string {
		b, _ := json.Marshal(href)
		return para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":` + string(b) + `}}]}`)
	}
	cases := []struct{ name, body, want string }{
		{"unknown node", `{"type":"doc","content":[{"type":"iframe"}]}`, `holds a "iframe" block`},
		{"unknown mark", para(`{"type":"text","text":"x","marks":[{"type":"underline"}]}`), `uses a "underline" style`},
		{"unknown attribute", `{"type":"doc","content":[{"type":"paragraph","attrs":{"style":"color:red"}}]}`, `attribute "style"`},
		{"unknown field", `{"type":"doc","html":"<script>","content":[]}`, `not a document`},
		{"heading too deep", `{"type":"doc","content":[{"type":"heading","attrs":{"level":5}}]}`, `level=5`},
		{"fractional level", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1.5}}]}`, `level=1.5`},
		{"level as string", `{"type":"doc","content":[{"type":"heading","attrs":{"level":"1"}}]}`, `level="1"`},
		{"anchor with markup", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1,"id":"a\"><img"}}]}`, `id=`},
		{"anchor uppercase", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1,"id":"Plan"}}]}`, `id="Plan"`},
		{"duplicate anchors", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1,"id":"a"}},{"type":"heading","attrs":{"level":2,"id":"a"}}]}`, `share the anchor "a"`},
		{"javascript link", link("javascript:alert(1)"), `href=`},
		{"mixed case scheme", link("JaVaScRiPt:alert(1)"), `href=`},
		{"tab in scheme", link("java\tscript:alert(1)"), `href=`},
		{"newline in scheme", link("java\nscript:alert(1)"), `href=`},
		{"leading space", link(" javascript:alert(1)"), `href=`},
		{"data url", link("data:text/html,<script>alert(1)</script>"), `href=`},
		{"vbscript", link("vbscript:msgbox"), `href=`},
		{"protocol relative", link("//evil.test/x"), `href=`},
		{"backslash host", link("/\\evil.test"), `href=`},
		{"http without host", link("http:evil"), `href=`},
		{"empty href", link(""), `href=`},
		{"overlong href", link("https://example.test/" + strings.Repeat("a", MaxHrefLength)), `href=`},
		{"link class", para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"/a","class":"evil"}}]}`), `class=`},
		{"link target", para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"/a","target":"_top"}}]}`), `target=`},
		{"panel kind", `{"type":"doc","content":[{"type":"panel","attrs":{"kind":"danger"},"content":[{"type":"paragraph"}]}]}`, `kind="danger"`},
		{"expand title too long", `{"type":"doc","content":[{"type":"expand","attrs":{"title":"` + strings.Repeat("a", MaxExpandTitleLength+1) + `"},"content":[{"type":"paragraph"}]}]}`, `title=`},
		{"expand title not text", `{"type":"doc","content":[{"type":"expand","attrs":{"title":3},"content":[{"type":"paragraph"}]}]}`, `title=3`},
		{"expand title null", `{"type":"doc","content":[{"type":"expand","attrs":{"title":null},"content":[{"type":"paragraph"}]}]}`, `title=null`},
		{"expand stored open", `{"type":"doc","content":[{"type":"expand","attrs":{"title":"More","open":true},"content":[{"type":"paragraph"}]}]}`, `attribute "open"`},
		{"decision state", `{"type":"doc","content":[{"type":"decision","attrs":{"state":"maybe"}}]}`, `state="maybe"`},
		{"decision holding a block", `{"type":"doc","content":[{"type":"decision","attrs":{"state":"decided"},"content":[{"type":"paragraph"}]}]}`, `puts a "paragraph"`},
		{"link card to a page of the site", `{"type":"doc","content":[{"type":"linkCard","attrs":{"url":"/spaces/x","view":"card"}}]}`, `url="/spaces/x"`},
		{"link card to a script", `{"type":"doc","content":[{"type":"linkCard","attrs":{"url":"javascript:alert(1)","view":"card"}}]}`, `url="javascript`},
		{"link card to mail", `{"type":"doc","content":[{"type":"linkCard","attrs":{"url":"mailto:a@example.test","view":"card"}}]}`, `url="mailto`},
		{"link card view", `{"type":"doc","content":[{"type":"linkCard","attrs":{"url":"https://example.test","view":"frame"}}]}`, `view="frame"`},
		{"link card in a line", para(`{"type":"linkCard","attrs":{"url":"https://example.test","view":"card"}}`), `puts a "linkCard"`},
		{"chart of a project in lower case", `{"type":"doc","content":[{"type":"armatureChart","attrs":{"project":"cp","query":"x","chart":"pie","groupBy":"type","days":30}}]}`, `project="cp"`},
		{"chart of a bar", `{"type":"doc","content":[{"type":"armatureChart","attrs":{"project":"CP","query":"x","chart":"bar","groupBy":"type","days":30}}]}`, `chart="bar"`},
		{"chart a year and a day back", `{"type":"doc","content":[{"type":"armatureChart","attrs":{"project":"CP","query":"x","chart":"createdResolved","groupBy":"type","days":366}}]}`, `days=366`},
		{"roadmap by sprint", `{"type":"doc","content":[{"type":"armatureRoadmap","attrs":{"project":"CP","query":"x","groupBy":"sprint"}}]}`, `groupBy="sprint"`},
		{"roadmap without a query", `{"type":"doc","content":[{"type":"armatureRoadmap","attrs":{"project":"CP","query":" ","groupBy":"team"}}]}`, `query`},
		{"properties without rows", `{"type":"doc","content":[{"type":"properties","content":[]}]}`, `properties`},
		{"properties holding a paragraph", `{"type":"doc","content":[{"type":"properties","content":[{"type":"paragraph"}]}]}`, `"paragraph"`},
		{"property row alone", `{"type":"doc","content":[{"type":"propertyRow","attrs":{"key":"Owner"}}]}`, `"propertyRow"`},
		{"property name too long", `{"type":"doc","content":[{"type":"properties","content":[{"type":"propertyRow","attrs":{"key":"` + strings.Repeat("x", MaxPropertyKeyLength+1) + `"}}]}]}`, `key=`},
		{"report without labels", `{"type":"doc","content":[{"type":"propertiesReport","attrs":{"labels":[],"space":null,"columns":[]}}]}`, `labels`},
		{"report of an upper case label", `{"type":"doc","content":[{"type":"propertiesReport","attrs":{"labels":["Release"],"space":null,"columns":[]}}]}`, `labels`},
		{"report of a lower case space", `{"type":"doc","content":[{"type":"propertiesReport","attrs":{"labels":["a"],"space":"docs","columns":[]}}]}`, `space="docs"`},
		{"report of a padded column", `{"type":"doc","content":[{"type":"propertiesReport","attrs":{"labels":["a"],"space":null,"columns":[" Owner"]}}]}`, `columns`},
		{"labelled pages matching some", `{"type":"doc","content":[{"type":"labelledPages","attrs":{"labels":["a"],"match":"some","space":null,"sort":"title","limit":5}}]}`, `match="some"`},
		{"labelled pages by views", `{"type":"doc","content":[{"type":"labelledPages","attrs":{"labels":["a"],"match":"all","space":null,"sort":"views","limit":5}}]}`, `sort="views"`},
		{"labelled pages without labels", `{"type":"doc","content":[{"type":"labelledPages","attrs":{"labels":[],"match":"all","space":null,"sort":"title","limit":5}}]}`, `labels`},
		{"labelled pages past the limit", `{"type":"doc","content":[{"type":"labelledPages","attrs":{"labels":["a"],"match":"all","space":null,"sort":"title","limit":51}}]}`, `limit=51`},
		{"recently updated none", `{"type":"doc","content":[{"type":"recentlyUpdated","attrs":{"space":null,"limit":0}}]}`, `limit=0`},
		{"task report for somebody", `{"type":"doc","content":[{"type":"taskReport","attrs":{"space":null,"assignee":"ann","due":"any","state":"open","limit":5}}]}`, `assignee="ann"`},
		{"task report due later", `{"type":"doc","content":[{"type":"taskReport","attrs":{"space":null,"assignee":null,"due":"later","state":"open","limit":5}}]}`, `due="later"`},
		{"task report half done", `{"type":"doc","content":[{"type":"taskReport","attrs":{"space":null,"assignee":null,"due":"any","state":"half","limit":5}}]}`, `state="half"`},
		{"task report past the limit", `{"type":"doc","content":[{"type":"taskReport","attrs":{"space":null,"assignee":null,"due":"any","state":"open","limit":101}}]}`, `limit=101`},
		{"calendar without its calendar", `{"type":"doc","content":[{"type":"calendar","attrs":{"calendarId":"team","project":null}}]}`, `calendarId="team"`},
		{"calendar of a project in lower case", `{"type":"doc","content":[{"type":"calendar","attrs":{"calendarId":"0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a77","project":"cp"}}]}`, `project="cp"`},
		{"calendar with its events", `{"type":"doc","content":[{"type":"calendar","attrs":{"calendarId":"0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a77","project":null,"events":[]}}]}`, `attribute "events"`},
		{"attachment list naming files", `{"type":"doc","content":[{"type":"attachmentList","attrs":{"files":[]}}]}`, `attribute "files"`},
		{"task report with tasks", `{"type":"doc","content":[{"type":"taskReport","attrs":{"space":null,"assignee":null,"due":"any","state":"open","limit":5,"tasks":[]}}]}`, `attribute "tasks"`},
		{"recently updated with pages", `{"type":"doc","content":[{"type":"recentlyUpdated","attrs":{"space":null,"limit":5,"pages":[]}}]}`, `attribute "pages"`},
		{"report with its rows", `{"type":"doc","content":[{"type":"propertiesReport","attrs":{"labels":["a"],"space":null,"columns":[],"rows":[]}}]}`, `attribute "rows"`},
		{"chart with counts", `{"type":"doc","content":[{"type":"armatureChart","attrs":{"project":"CP","query":"x","chart":"pie","groupBy":"type","days":30,"total":5}}]}`, `attribute "total"`},
		{"include of no page", `{"type":"doc","content":[{"type":"include","attrs":{"pageId":"HOME","excerptId":null}}]}`, `pageId="HOME"`},
		{"include in a line", para(`{"type":"include","attrs":{"pageId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80"}}`), `puts a "include"`},
		{"include with words", `{"type":"doc","content":[{"type":"include","attrs":{"pageId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80","title":"Stale"}}]}`, `attribute "title"`},
		{"diagram without source", `{"type":"doc","content":[{"type":"diagram","attrs":{"source":"\n "}}]}`, `source="\n "`},
		{"diagram too long", `{"type":"doc","content":[{"type":"diagram","attrs":{"source":"` + strings.Repeat("x", MaxDiagramLength+1) + `"}}]}`, `source="xxx`},
		{"diagram in a line", para(`{"type":"diagram","attrs":{"source":"x"}}`), `puts a "diagram"`},
		{"formula without source", para(`{"type":"mathInline","attrs":{"latex":" "}}`), `latex=" "`},
		{"formula not text", `{"type":"doc","content":[{"type":"mathBlock","attrs":{"latex":42}}]}`, `latex=42`},
		{"formula too long", `{"type":"doc","content":[{"type":"mathBlock","attrs":{"latex":"` + strings.Repeat("x", MaxMathLength+1) + `"}}]}`, `latex="xxx`},
		{"formula holding text", `{"type":"doc","content":[{"type":"mathBlock","attrs":{"latex":"x"},"content":[{"type":"text","text":"x"}]}]}`, `puts content inside a "mathBlock"`},
		{"block formula in a line", para(`{"type":"mathBlock","attrs":{"latex":"x"}}`), `puts a "mathBlock"`},
		{"inline formula as a block", `{"type":"doc","content":[{"type":"mathInline","attrs":{"latex":"x"}}]}`, `puts a "mathInline"`},
		{"expand inline", para(`{"type":"expand","attrs":{"title":"More"}}`), `puts a "expand"`},
		{"one column", columns(column(`null`)), `a "columns" holding 1,`},
		{"four columns", columns(column(`null`), column(`null`), column(`null`), column(`null`)), `a "columns" holding 4,`},
		{"no columns", `{"type":"doc","content":[{"type":"columns","content":[]}]}`, `a "columns" holding 0,`},
		{"column too narrow", columns(column(`9`), column(`91`)), `width=9`},
		{"column too wide", columns(column(`81`), column(`19`)), `width=81`},
		{"column width not whole", columns(column(`50.5`), column(`49.5`)), `width=50.5`},
		{"column width as text", columns(column(`"50%"`), column(`50`)), `width="50%"`},
		{"column outside columns", `{"type":"doc","content":[{"type":"column","content":[{"type":"paragraph"}]}]}`, `puts a "column"`},
		{"paragraph in columns", `{"type":"doc","content":[{"type":"columns","content":[{"type":"paragraph"},{"type":"paragraph"}]}]}`, `puts a "paragraph"`},
		{"cell background colour", `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":{"background":"#ff0000"},"content":[{"type":"paragraph"}]}]}]}]}`, `background=`},
		{"cell align", `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":{"align":"justify;color:red"},"content":[{"type":"paragraph"}]}]}]}]}`, `align=`},
		{"huge colspan", `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colspan":100000},"content":[{"type":"paragraph"}]}]}]}]}`, `colspan=`},
		{"colwidth not numbers", `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","attrs":{"colwidth":["1"]},"content":[{"type":"paragraph"}]}]}]}]}`, `colwidth=`},
		{"code language injection", `{"type":"doc","content":[{"type":"codeBlock","attrs":{"language":"go\" onload=\"x"}}]}`, `language=`},
		{"marks inside code", `{"type":"doc","content":[{"type":"codeBlock","content":[{"type":"text","text":"x","marks":[{"type":"bold"}]}]}]}`, `styles a "text"`},
		{"marks on a block", `{"type":"doc","content":[{"type":"paragraph","marks":[{"type":"bold"}]}]}`, `styles a "paragraph"`},
		{"repeated mark", para(`{"type":"text","text":"x","marks":[{"type":"bold"},{"type":"bold"}]}`), `twice`},
		{"text at the top", `{"type":"doc","content":[{"type":"text","text":"x"}]}`, `puts a "text" where it cannot go`},
		{"cell outside a table", `{"type":"doc","content":[{"type":"tableCell","content":[{"type":"paragraph"}]}]}`, `puts a "tableCell"`},
		{"nested doc", `{"type":"doc","content":[{"type":"doc"}]}`, `puts a "doc"`},
		{"content in a leaf", `{"type":"doc","content":[{"type":"horizontalRule","content":[{"type":"paragraph"}]}]}`, `holds none`},
		{"text on a block", `{"type":"doc","content":[{"type":"paragraph","text":"x"}]}`, `text of its own`},
		{"empty text", para(`{"type":"text","text":""}`), `empty piece of text`},
		{"mention without id", para(`{"type":"mention","attrs":{"id":"","label":"x"}}`), `id=""`},
		{"mention id not a uuid", para(`{"type":"mention","attrs":{"id":"u1","label":"Ada"}}`), `id="u1"`},
		{"mention id upper case", para(`{"type":"mention","attrs":{"id":"0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A01","label":"Ada"}}`), `id=`},
		{"mention id with a path", para(`{"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01/../x","label":"Ada"}}`), `id=`},
		{"mention without label", para(`{"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01","label":"   "}}`), `label=`},
		{"image id not a uuid", `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"../../etc"}}]}`, `attachmentId=`},
		{"image id upper case", `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A5B"}}]}`, `attachmentId=`},
		{"image alt too long", `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","alt":"` + strings.Repeat("a", MaxAltLength+1) + `"}}]}`, `alt=`},
		{"image width zero", `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","width":0}}]}`, `width=0`},
		{"image src", `{"type":"doc","content":[{"type":"image","attrs":{"src":"https://evil.test/x.png"}}]}`, `attribute "src"`},
		{"image inline", para(`{"type":"image","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}}`), `puts a "image"`},
		{"attachment at the top", `{"type":"doc","content":[{"type":"attachment","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","fileName":"a"}}]}`, `puts a "attachment"`},
		{"attachment without name", para(`{"type":"attachment","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","fileName":" "}}`), `fileName=`},
		{"issue key lower case", para(`{"type":"armatureIssue","attrs":{"key":"cp-12"}}`), `key="cp-12"`},
		{"issue key without number", para(`{"type":"armatureIssue","attrs":{"key":"CP-"}}`), `key="CP-"`},
		{"issue key numbered from zero", para(`{"type":"armatureIssue","attrs":{"key":"CP-012"}}`), `key="CP-012"`},
		{"issue with a summary", para(`{"type":"armatureIssue","attrs":{"key":"CP-12","summary":"secret"}}`), `attribute "summary"`},
		{"issue at the top", `{"type":"doc","content":[{"type":"armatureIssue","attrs":{"key":"CP-12"}}]}`, `puts a "armatureIssue"`},
		{"issue block key lower case", `{"type":"doc","content":[{"type":"armatureIssueBlock","attrs":{"key":"cp-7"}}]}`, `key="cp-7"`},
		{"issue block without a key", `{"type":"doc","content":[{"type":"armatureIssueBlock","attrs":{"key":null}}]}`, `key=null`},
		{"issue block with a summary", `{"type":"doc","content":[{"type":"armatureIssueBlock","attrs":{"key":"CP-7","summary":"secret"}}]}`, `attribute "summary"`},
		{"issue block with content", `{"type":"doc","content":[{"type":"armatureIssueBlock","attrs":{"key":"CP-7"},"content":[{"type":"paragraph"}]}]}`, `holds none`},
		{"issue block inline", para(`{"type":"armatureIssueBlock","attrs":{"key":"CP-7"}}`), `puts a "armatureIssueBlock"`},
		{"issue list blank query", list(`"query":"  ","columns":["key"],"limit":20`), `query="  "`},
		{"issue list long query", list(`"query":"` + strings.Repeat("a", 2001) + `","columns":["key"],"limit":20`), `query=`},
		{"issue list no columns", list(`"query":"project = CP","columns":[],"limit":20`), `columns=[]`},
		{"issue list unknown column", list(`"query":"project = CP","columns":["key","storyPoints"],"limit":20`), `columns=`},
		{"issue list column twice", list(`"query":"project = CP","columns":["key","key"],"limit":20`), `columns=`},
		{"issue list columns as text", list(`"query":"project = CP","columns":"key","limit":20`), `columns="key"`},
		{"issue list column not text", list(`"query":"project = CP","columns":[1],"limit":20`), `columns=[1]`},
		{"issue list eleven columns", list(`"query":"project = CP","columns":["key","summary","type","status","priority","assignee","reporter","created","updated","due","key"],"limit":20`), `columns=`},
		{"issue list limit zero", list(`"query":"project = CP","columns":["key"],"limit":0`), `limit=0`},
		{"issue list limit too high", list(`"query":"project = CP","columns":["key"],"limit":101`), `limit=101`},
		{"issue list with rows", list(`"query":"project = CP","columns":["key"],"limit":20,"issues":[{"key":"CP-1"}]`), `attribute "issues"`},
		{"issue list inline", para(`{"type":"armatureIssueList","attrs":{"query":"project = CP","columns":["key"],"limit":20}}`), `puts a "armatureIssueList"`},
		{"contents level zero", `{"type":"doc","content":[{"type":"tableOfContents","attrs":{"maxLevel":0}}]}`, `maxLevel=0`},
		{"contents level too deep", `{"type":"doc","content":[{"type":"tableOfContents","attrs":{"maxLevel":4}}]}`, `maxLevel=4`},
		{"contents level as string", `{"type":"doc","content":[{"type":"tableOfContents","attrs":{"maxLevel":"2"}}]}`, `maxLevel="2"`},
		{"contents level missing a value", `{"type":"doc","content":[{"type":"tableOfContents","attrs":{"maxLevel":null}}]}`, `maxLevel=null`},
		{"contents with text", `{"type":"doc","content":[{"type":"tableOfContents","content":[{"type":"paragraph"}]}]}`, `holds none`},
		{"contents inline", para(`{"type":"tableOfContents","attrs":{"maxLevel":2}}`), `puts a "tableOfContents"`},
		{"contents unknown attribute", `{"type":"doc","content":[{"type":"tableOfContents","attrs":{"headings":["a"]}}]}`, `attribute "headings"`},
		{"child pages scope", `{"type":"doc","content":[{"type":"childPages","attrs":{"scope":"space"}}]}`, `scope="space"`},
		{"child pages sort", `{"type":"doc","content":[{"type":"childPages","attrs":{"sort":"created"}}]}`, `sort="created"`},
		{"child pages sort null", `{"type":"doc","content":[{"type":"childPages","attrs":{"sort":null}}]}`, `sort=null`},
		{"child pages depth zero", `{"type":"doc","content":[{"type":"childPages","attrs":{"depth":0}}]}`, `depth=0`},
		{"child pages depth too deep", `{"type":"doc","content":[{"type":"childPages","attrs":{"depth":11}}]}`, `depth=11`},
		{"child pages fractional depth", `{"type":"doc","content":[{"type":"childPages","attrs":{"depth":1.5}}]}`, `depth=1.5`},
		{"child pages of another page", `{"type":"doc","content":[{"type":"childPages","attrs":{"pageId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}}]}`, `attribute "pageId"`},
		{"child pages inline", para(`{"type":"childPages","attrs":{"scope":"children"}}`), `puts a "childPages"`},
		{"attrs not an object", `{"type":"doc","content":[{"type":"paragraph","attrs":[1]}]}`, `not a document`},
		{"not a doc", `{"type":"paragraph"}`, `must be a document`},
		{"not json", `not json`, `not a document`},
		{"trailing data", `{"type":"doc"}{"type":"doc"}`, `not a document`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(json.RawMessage(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%v does not wrap ErrInvalid", err)
			}
		})
	}
}

func TestValidateBoundsDepthAndSize(t *testing.T) {
	nest := func(levels int) string {
		return `{"type":"doc","content":[` + strings.Repeat(`{"type":"blockquote","content":[`, levels) + `{"type":"paragraph"}` + strings.Repeat(`]}`, levels) + `]}`
	}
	if err := Validate(json.RawMessage(nest(MaxDepth - 2))); err != nil {
		t.Fatalf("a nest within the limit was refused: %v", err)
	}
	if err := Validate(json.RawMessage(nest(MaxDepth + 1))); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("a deep nest was accepted: %v", err)
	}
	// Deeper than the JSON decoder itself will go: refused, never a crash.
	if err := Validate(json.RawMessage(nest(20000))); err == nil {
		t.Fatal("a pathological nest was accepted")
	}
	big := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + strings.Repeat("a", MaxBytes) + `"}]}]}`
	if err := Validate(json.RawMessage(big)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("an oversized page was accepted: %v", err)
	}
}

func TestSafeHref(t *testing.T) {
	for _, ok := range []string{"https://example.test/a?b=c#d", "http://example.test", "HTTPS://EXAMPLE.TEST", "mailto:ada@example.test", "/spaces/eng/pages/1", "#plan", "?q=1", "relative/page", "./here:colon", "../up"} {
		if !SafeHref(ok) {
			t.Errorf("%q was refused", ok)
		}
	}
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "ftp://example.test", "mailto:", "https://", "//evil.test", "https://ex ample.test", "a\x00b", "foo:bar"} {
		if SafeHref(bad) {
			t.Errorf("%q was admitted", bad)
		}
	}
}

func TestPlainTextReadsEveryBlock(t *testing.T) {
	root, err := Parse(json.RawMessage(richDoc))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"Plan",
		"Ask @Ada Lovelace first",
		"x := 1Say moresite",
		"one",
		"three",
		"done",
		"quoted",
		"x := 1",
		"y := 2",
		"See report.pdf",
		"Fixed in CP-12",
		"CP-7",
		"Owner\t@Ada",
		"\tFinal",
		"Due",
		"Name\tRole",
		"Ada",
		"Careful",
		"Hot",
		"Rollback steps",
		"Revert the release",
		"Nested detail",
		"Left side",
		"Right side",
		"Ship weekly",
		"Energy is E = mc^2",
		`\int_0^1 x\,dx = \frac{1}{2}`,
		"flowchart LR",
		"  A[Draft] --> B[Published]",
		"Nine to five",
		"Plan",
	}, "\n")
	if got := PlainText(root); got != want {
		t.Errorf("PlainText =\n%s\nwant\n%s", got, want)
	}
}

func TestHeadingsKeepSavedAnchorsAndFillMissingOnes(t *testing.T) {
	root, err := Parse(json.RawMessage(richDoc))
	if err != nil {
		t.Fatal(err)
	}
	want := []Heading{
		{Level: 1, Anchor: "plan", Text: "Plan"},
		{Level: 2, Anchor: "careful", Text: "Careful"},
		{Level: 3, Anchor: "plan-2", Text: "Plan"},
	}
	if got := Headings(root); !reflect.DeepEqual(got, want) {
		t.Errorf("Headings = %+v, want %+v", got, want)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Getting Started":          "getting-started",
		"  What's new in 2.0?  ":   "what-s-new-in-2-0",
		"Übersicht & Ziele":        "übersicht-ziele",
		"!!!":                      FallbackSlug,
		"":                         FallbackSlug,
		"日本語の見出し":                  "日本語の見出し",
		strings.Repeat("ab ", 100): strings.Repeat("ab-", 21) + "a",
	}
	for in, want := range cases {
		got := Slug(in)
		if got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if !patterns[AnchorPattern].MatchString(got) {
			t.Errorf("Slug(%q) = %q does not match the anchor pattern", in, got)
		}
	}
	taken := map[string]bool{"a": true, "a-2": true}
	if got := Dedupe("a", taken); got != "a-3" {
		t.Errorf("Dedupe = %q", got)
	}
}

// The web editor's test reads the committed copy; it has to be this table.
func TestAllowlistFileIsCurrent(t *testing.T) {
	committed, err := os.ReadFile("../../../api/document-allowlist.json")
	if err != nil {
		t.Fatalf("api/document-allowlist.json is missing; run make document-allowlist: %v", err)
	}
	fresh, err := Allowed.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(committed) != string(fresh) {
		t.Fatal("api/document-allowlist.json is out of date; run make document-allowlist and commit it.")
	}
}

func columns(cols ...string) string {
	return `{"type":"doc","content":[{"type":"columns","content":[` + strings.Join(cols, ",") + `]}]}`
}

func column(width string) string {
	return `{"type":"column","attrs":{"width":` + width + `},"content":[{"type":"paragraph"}]}`
}
