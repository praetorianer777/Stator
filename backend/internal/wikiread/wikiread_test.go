package wikiread

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

var limits = Limits{Pages: 5000, Versions: 100000, Files: 20000, Unpacked: 1 << 30}

func zipOf(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(files[name]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func byTitle(sp *Space) map[string]*Page {
	out := map[string]*Page{}
	for _, p := range sp.Pages {
		out[p.Title] = p
	}
	return out
}

func titles(sp *Space) []string {
	var out []string
	for _, p := range sp.Pages {
		out = append(out, p.Title)
	}
	return out
}

func current(p *Page) string { return string(p.Versions[len(p.Versions)-1].Body) }

func lossKinds(sp *Space) map[LossKind][]string {
	out := map[LossKind][]string{}
	for _, l := range sp.Losses {
		out[l.Kind] = append(out[l.Kind], l.Page+": "+l.Detail)
	}
	return out
}

// page is a page file of an HTML export in the structure the reader follows:
// breadcrumbs, a title, who wrote it, the content, labels and comments.
func page(title, crumbs, meta, content, labels, comments string) string {
	return `<!DOCTYPE html><html><head><title>Travel Guide : ` + title + `</title></head><body>
<div id="breadcrumbs"><ul>` + crumbs + `</ul></div>
<h1 id="title-heading"><span id="title-text">Travel Guide : ` + title + `</span></h1>
<div class="page-metadata">` + meta + `</div>
<div id="main-content" class="wiki-content">` + content + `</div>
<div class="labels"><h2>Labels:</h2><ul>` + labels + `</ul></div>
<div id="comments-section">` + comments + `</div>
<div id="footer">Made by a wiki</div>
</body></html>`
}

func htmlExport() map[string]string {
	return map[string]string{
		"guide/index.html": `<html><head><title>Travel Guide</title></head><body><div id="main-content">
<h1>Travel Guide</h1><p>Everything about our trips.</p>
<h2>Available pages:</h2>
<ul><li><a href="Home_100.html">Home</a><ul>
  <li><a href="Packing_101.html">Packing</a><ul><li><a href="Checklist_102.html">Checklist</a></li></ul></li>
  <li><a href="Routes_103.html">Routes</a></li>
</ul></li></ul></div></body></html>`,
		"guide/Home_100.html": page("Home", `<li><a href="index.html">Travel Guide</a></li>`,
			`Created by <span class="author"><a href="mailto:ann@example.com">Ann Example</a></span> on <time datetime="2026-01-02">Jan 02, 2026</time>`,
			`<p>Welcome. Read <a href="Packing_101.html">how to pack</a> first.</p>`, "", ""),
		"guide/Packing_101.html": page("Packing", `<li><a href="index.html">Travel Guide</a></li><li><a href="Home_100.html">Home</a></li>`,
			`Created by <span class="author">Ann Example</span>, last modified by <span class="editor"><a href="mailto:bob@example.com">Bob Example</a></span> on Feb 03, 2026`,
			`<h1>Before you go</h1>
<p>Pack <strong>light</strong> and <em>early</em>.<br/>Really.</p>
<p><span class="image-wrap"><img class="image" src="attachments/101/201.png?width=300" width="300" alt="The map"></span></p>
<div class="callout callout-warning"><span class="icon icon-warning">Icon</span><div class="callout-body"><p>Mind the weight limit.</p></div></div>
<div class="table-wrap"><table><tbody><tr><th>Item</th><th>Count</th></tr><tr><td>Socks</td><td colspan="1">7</td></tr></tbody></table></div>
<pre class="syntaxhighlighter-pre" data-syntaxhighlighter-params="brush: bash; gutter: false">tar czf trip.tgz .
echo done</pre>
<ul><li>Shoes<ul><li>Boots</li></ul></li><li>Hat</li></ul>
<ul class="task-list"><li class="checked">Passport</li><li>Tickets</li></ul>
<p>See the <a href="Routes_103.html#north">routes</a>, the <a href="attachments/101/202.pdf">plan.pdf</a> and <a href="Elsewhere_999.html">a page we lost</a>.</p>
<p><img src="https://pictures.example.com/sun.png" alt="The sun"></p>
<iframe src="https://video.example.com/embed/1"></iframe>
<div class="expand-container"><div class="expand-control"><span class="expand-control-text">More tips</span></div><div class="expand-content"><p>Roll your shirts.</p></div></div>`,
			`<li><a href="labels/packing.html">packing</a></li><li><a href="labels/howto.html">How To</a></li>`,
			`<div class="comment"><span class="author">Bob Example</span><time datetime="2026-02-04T10:00:00Z">Feb 4</time>
  <div class="comment-body"><p>Do not forget <strong>socks</strong>.</p><table><tr><td>a</td><td>b</td></tr></table></div>
  <div class="comment"><span class="author"><a href="mailto:ann@example.com">Ann Example</a></span><time datetime="2026-02-05T10:00:00Z">Feb 5</time>
    <div class="comment-body"><p>Never.</p></div></div>
</div>`),
		"guide/Checklist_102.html": page("Checklist", "", "", `<ol start="3"><li>Water</li><li>Snacks</li></ol>`, "", ""),
		"guide/Routes_103.html":    page("Routes", "", "", `<h2>North</h2><p>Up the hill.</p>`, "", ""),
		"guide/Loose_104.html": page("Loose", `<li><a href="index.html">Travel Guide</a></li><li><a href="Home_100.html">Home</a></li><li><a href="Packing_101.html">Packing</a></li>`,
			"", `<p>Not in the tree.</p>`, "", ""),
		"guide/attachments/101/201.png": "not really a picture",
		"guide/attachments/101/202.pdf": "%PDF-1.4 plan",
		"guide/styles/site.css":         "body{}",
	}
}

// An HTML export is read into its tree, each page's content held to the
// allowlist, its pictures and files, labels and comments, and the losses.
func TestAnHTMLExportIsReadWithItsTreeFilesLabelsAndComments(t *testing.T) {
	zr := zipOf(t, htmlExport())
	if got := Detect(zr); got != FormatHTML {
		t.Fatalf("the export is detected as %q", got)
	}
	sp, err := Read(zr, FormatHTML, Options{Key: "TRIP", Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	if sp.Name != "Travel Guide" {
		t.Errorf("the space is called %q", sp.Name)
	}
	var order []string
	for _, p := range sp.Pages {
		order = append(order, p.Title)
	}
	if !slices.Equal(order, []string{"Home", "Packing", "Checklist", "Loose", "Routes"}) {
		t.Errorf("the pages are made in the order %v", order)
	}
	pages := byTitle(sp)
	home, packing := pages["Home"], pages["Packing"]
	if home.Parent != nil || *packing.Parent != home.ID || *pages["Checklist"].Parent != packing.ID || *pages["Loose"].Parent != packing.ID || *pages["Routes"].Parent != home.ID {
		t.Errorf("the tree is not the index's and the breadcrumbs'")
	}
	for _, p := range sp.Pages {
		for _, v := range p.Versions {
			if err := document.Validate(v.Body); err != nil {
				t.Errorf("%s's body is refused: %v\n%s", p.Title, err, v.Body)
			}
		}
		for _, th := range p.Threads {
			for _, c := range th.Comments {
				if _, err := document.ParseComment(c.Body); err != nil {
					t.Errorf("a comment on %s is refused: %v\n%s", p.Title, err, c.Body)
				}
			}
		}
	}

	body := current(packing)
	if len(packing.Files) != 2 {
		t.Fatalf("Packing has %d files", len(packing.Files))
	}
	var picture, plan File
	for _, f := range packing.Files {
		switch f.Name {
		case "201.png":
			picture = f
		case "plan.pdf":
			plan = f
		}
	}
	if picture.ID.String() == "00000000-0000-0000-0000-000000000000" || plan.ContentType != "application/pdf" {
		t.Fatalf("the files read as %+v", packing.Files)
	}
	data, err := plan.Read()
	if err != nil || string(data) != "%PDF-1.4 plan" {
		t.Errorf("the plan reads %q, %v", data, err)
	}
	for _, want := range []string{
		`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Before you go"}]}`,
		`{"type":"text","text":"light","marks":[{"type":"bold"}]}`,
		`{"type":"hardBreak"}`,
		`{"type":"image","attrs":{"alt":"The map","attachmentId":"` + picture.ID.String() + `","width":300}}`,
		`{"type":"panel","attrs":{"kind":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Mind the weight limit."}]}]}`,
		`"type":"tableHeader"`,
		`{"type":"codeBlock","attrs":{"language":"bash"},"content":[{"type":"text","text":"tar czf trip.tgz .\necho done"}]}`,
		`{"type":"taskItem","attrs":{"checked":true}`,
		`"href":"/s/TRIP/p/` + pages["Routes"].ID.String() + `"`,
		`{"type":"attachment","attrs":{"attachmentId":"` + plan.ID.String() + `","fileName":"plan.pdf"}}`,
		`{"type":"text","text":" and a page we lost."}`,
		`{"type":"text","text":"The sun","marks":[{"type":"link","attrs":{"href":"https://pictures.example.com/sun.png"}}]}`,
		`{"type":"expand","attrs":{"title":"More tips"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Packing lacks %s:\n%s", want, body)
		}
	}
	for _, gone := range []string{"Icon", "Made by a wiki", "Labels:", "Do not forget", "Travel Guide : Packing", "iframe"} {
		if strings.Contains(body, gone) {
			t.Errorf("Packing holds %q:\n%s", gone, body)
		}
	}
	if !strings.Contains(current(home), `"href":"/s/TRIP/p/`+packing.ID.String()+`"`) {
		t.Errorf("Home's link to Packing is not rewritten: %s", current(home))
	}
	if !strings.Contains(current(pages["Checklist"]), `"start":3`) {
		t.Errorf("the numbered list does not start at 3: %s", current(pages["Checklist"]))
	}

	if !slices.Equal(packing.Labels, []string{"packing", "How To"}) {
		t.Errorf("Packing's labels are %v", packing.Labels)
	}
	if len(packing.Threads) != 1 || len(packing.Threads[0].Comments) != 2 {
		t.Fatalf("Packing's discussion is %+v", packing.Threads)
	}
	first, reply := packing.Threads[0].Comments[0], packing.Threads[0].Comments[1]
	if !strings.Contains(string(first.Body), "socks") || !strings.Contains(string(first.Body), "a | b") || !strings.Contains(string(reply.Body), "Never.") {
		t.Errorf("the comments read %s and %s", first.Body, reply.Body)
	}
	people := map[string]Person{}
	for _, p := range sp.People {
		people[p.Ref] = p
	}
	if people[reply.By].Email != "ann@example.com" || people[first.By].Name != "Bob Example" || people[first.By].Email != "" {
		t.Errorf("the comments are by %+v and %+v", people[first.By], people[reply.By])
	}
	if people[packing.Versions[0].By].Email != "bob@example.com" || people[home.CreatedBy].Email != "ann@example.com" {
		t.Errorf("Packing's version is by %+v, Home by %+v", people[packing.Versions[0].By], people[home.CreatedBy])
	}
	if packing.Versions[0].At.Format("2006-01-02") != "2026-02-03" || home.CreatedAt.Format("2006-01-02") != "2026-01-02" {
		t.Errorf("the dates read %s and %s", packing.Versions[0].At, home.CreatedAt)
	}

	losses := lossKinds(sp)
	for kind, want := range map[LossKind]string{
		LossOutsideLink:   "Packing: Elsewhere_999.html",
		LossExternalImage: "Packing: https://pictures.example.com/sun.png",
		LossEmbed:         "Packing: iframe",
	} {
		if !slices.Contains(losses[kind], want) {
			t.Errorf("the losses of kind %s are %v, lacking %q", kind, losses[kind], want)
		}
	}
	if sp.LostCount != len(sp.Losses) {
		t.Errorf("%d losses are counted and %d listed", sp.LostCount, len(sp.Losses))
	}
}

// An index whose tree has several roots gets a home page of its own, made
// of the index's words and listing the pages below it.
func TestAnIndexOfSeveralRootsBecomesTheHomePage(t *testing.T) {
	zr := zipOf(t, map[string]string{
		"index.html": `<html><head><title>Notes</title></head><body><p>Our notes.</p><ul><li><a href="a.html">A</a></li><li><a href="b.html">B</a></li></ul></body></html>`,
		"a.html":     `<html><head><title>Notes : A</title></head><body><h1>A</h1><p>a</p></body></html>`,
		"b.html":     `<html><head><title>Notes : B</title></head><body><p>b</p></body></html>`,
	})
	sp, err := Read(zr, FormatHTML, Options{Key: "N", Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	if len(sp.Pages) != 3 || sp.Pages[0].Title != "Notes" || sp.Pages[1].Title != "A" || sp.Pages[2].Title != "B" {
		t.Fatalf("the pages are %v", titles(sp))
	}
	home := current(sp.Pages[0])
	if !strings.Contains(home, "Our notes.") || !strings.Contains(home, `"type":"childPages"`) || strings.Contains(home, `"text":"A"`) {
		t.Errorf("the home page reads %s", home)
	}
	if strings.Contains(current(sp.Pages[1]), `"heading"`) {
		t.Errorf("A shows its title twice: %s", current(sp.Pages[1]))
	}
	if *sp.Pages[1].Parent != sp.Pages[0].ID || *sp.Pages[2].Parent != sp.Pages[0].ID {
		t.Errorf("the roots do not hang from the home page")
	}
}

func entities(objects ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<export>` + strings.Join(objects, "\n") + `</export>`
}

func obj(class, id string, props ...string) string {
	return fmt.Sprintf(`<object class=%q package="org.example"><id name="id">%s</id>%s</object>`, class, id, strings.Join(props, ""))
}

func text(name, value string) string {
	return fmt.Sprintf(`<property name=%q><![CDATA[%s]]></property>`, name, strings.ReplaceAll(value, "]]>", "]]]]><![CDATA[>"))
}

func ref(name, class, id string) string {
	return fmt.Sprintf(`<property name=%q class=%q package="org.example"><id name="id">%s</id></property>`, name, class, id)
}

func keyRef(name, key string) string {
	return fmt.Sprintf(`<property name=%q class="User" package="org.example"><id name="key">%s</id></property>`, name, key)
}

func xmlExport() map[string]string {
	return map[string]string{
		"entities.xml": entities(
			obj("Space", "1", text("key", "OLD"), text("name", "Engineering"), ref("homePage", "Page", "10"), ref("description", "SpaceDescription", "2")),
			obj("SpaceDescription", "2", ref("space", "Space", "1")),
			obj("BodyContent", "3", text("body", "<p>How we build things.</p>"), ref("content", "SpaceDescription", "2"), text("bodyType", "2")),
			obj("User", "k-ann", text("name", "ann"), text("email", "ann@example.com"), text("fullName", "Ann Example")),
			obj("User", "k-bob", text("name", "bob"), text("fullName", "Bob Example")),
			obj("Page", "10", text("title", "Engineering Home"), ref("space", "Space", "1"), text("version", "1"), text("contentStatus", "current"),
				keyRef("creator", "k-ann"), text("creationDate", "2026-01-01 09:00:00.000"), keyRef("lastModifier", "k-ann"), text("lastModificationDate", "2026-01-01 09:00:00.000")),
			obj("BodyContent", "100", text("body", `<p>Start with <ac:link><ri:page ri:content-title="Build" /></ac:link>.</p><ac:structured-macro ac:name="children" />`), ref("content", "Page", "10"), text("bodyType", "2")),
			obj("Page", "11", text("title", "Build"), ref("space", "Space", "1"), ref("parent", "Page", "10"), text("position", "0"), text("version", "2"),
				text("contentStatus", "current"), keyRef("creator", "k-bob"), text("creationDate", "2026-02-01 09:00:00.000"),
				keyRef("lastModifier", "k-ann"), text("lastModificationDate", "2026-03-01 09:00:00.000"), text("versionComment", "Added steps")),
			obj("BodyContent", "110", ref("content", "Page", "11"), text("bodyType", "2"), text("body",
				`<h2>Steps</h2>
<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[go build ./...]]></ac:plain-text-body></ac:structured-macro>
<ac:structured-macro ac:name="info"><ac:parameter ac:name="title">Heads up</ac:parameter><ac:rich-text-body><p>CI runs it too.</p></ac:rich-text-body></ac:structured-macro>
<ac:structured-macro ac:name="expand"><ac:parameter ac:name="title">Details</ac:parameter><ac:rich-text-body><p>More words.</p></ac:rich-text-body></ac:structured-macro>
<p>State: <ac:structured-macro ac:name="status"><ac:parameter ac:name="colour">Green</ac:parameter><ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro> by <ac:link><ri:user ri:userkey="k-ann" /></ac:link>.</p>
<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>Write it</ac:task-body></ac:task><ac:task><ac:task-status>incomplete</ac:task-status><ac:task-body>Ship it</ac:task-body></ac:task></ac:task-list>
<p><ac:image ac:width="200"><ri:attachment ri:filename="diagram.png" /></ac:image></p>
<p><ac:link><ri:attachment ri:filename="notes.txt" /></ac:link> and <ac:link><ri:page ri:space-key="ELSE" ri:content-title="Other" /><ac:plain-text-link-body><![CDATA[elsewhere]]></ac:plain-text-link-body></ac:link></p>
<ac:structured-macro ac:name="roadmap"><ac:parameter ac:name="x">y</ac:parameter></ac:structured-macro>
<ac:layout><ac:layout-section ac:type="two_equal"><ac:layout-cell><p>Left</p></ac:layout-cell><ac:layout-cell><p>Right</p></ac:layout-cell></ac:layout-section></ac:layout>
<p>Done&nbsp;<ac:emoticon ac:name="smile" /></p>`)),
			obj("Page", "12", text("title", "Build"), ref("space", "Space", "1"), text("version", "1"), ref("originalVersion", "Page", "11"),
				text("contentStatus", "current"), keyRef("creator", "k-bob"), text("creationDate", "2026-02-01 09:00:00.000"),
				keyRef("lastModifier", "k-bob"), text("lastModificationDate", "2026-02-01 09:00:00.000")),
			obj("BodyContent", "120", ref("content", "Page", "12"), text("bodyType", "2"), text("body", "<p>First draft.</p>")),
			obj("Page", "13", text("title", "Draft only"), ref("space", "Space", "1"), text("contentStatus", "draft")),
			obj("BlogPost", "14", text("title", "Release notes"), ref("space", "Space", "1"), text("contentStatus", "current"),
				keyRef("creator", "k-ann"), text("creationDate", "2026-04-01 09:00:00.000")),
			obj("BodyContent", "140", ref("content", "BlogPost", "14"), text("bodyType", "2"), text("body", "<p>We shipped.</p>")),
			obj("Attachment", "20", text("title", "diagram.png"), ref("containerContent", "Page", "11"), text("version", "2"), text("contentStatus", "current"),
				text("contentType", "image/png"), keyRef("creator", "k-ann"), text("creationDate", "2026-02-02 09:00:00.000")),
			obj("Attachment", "21", text("title", "diagram.png"), ref("containerContent", "Page", "11"), text("version", "1"), ref("originalVersion", "Attachment", "20"),
				text("contentStatus", "current"), keyRef("creator", "k-bob"), text("creationDate", "2026-02-01 09:00:00.000")),
			obj("Attachment", "22", text("title", "notes.txt"), ref("containerContent", "Page", "11"), text("version", "1"), text("contentStatus", "current")),
			obj("ContentProperty", "30", text("name", "MEDIA_TYPE"), text("stringValue", "text/plain"), ref("content", "Attachment", "22")),
			obj("Comment", "40", ref("containerContent", "Page", "11"), text("contentStatus", "current"), keyRef("creator", "k-bob"), text("creationDate", "2026-03-02 09:00:00.000")),
			obj("BodyContent", "400", ref("content", "Comment", "40"), text("bodyType", "2"), text("body", "<p>Looks <strong>good</strong>.</p>")),
			obj("Comment", "41", ref("containerContent", "Page", "11"), ref("parent", "Comment", "40"), text("contentStatus", "current"), keyRef("creator", "k-ann"), text("creationDate", "2026-03-03 09:00:00.000")),
			obj("BodyContent", "410", ref("content", "Comment", "41"), text("bodyType", "2"), text("body", `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Thanks.</p></ac:rich-text-body></ac:structured-macro>`)),
			obj("Label", "50", text("name", "build"), text("namespace", "global")),
			obj("Label", "51", text("name", "mine"), text("namespace", "my")),
			obj("Labelling", "52", ref("label", "Label", "50"), ref("content", "Page", "11")),
			obj("Labelling", "53", ref("label", "Label", "51"), ref("content", "Page", "11")),
			obj("Unrelated", "60", text("noise", "kept out")),
		),
		"attachments/11/20/1": "first diagram",
		"attachments/11/20/2": "second diagram",
		"attachments/11/22/1": "plain notes",
	}
}

// An XML export is read with every version, comment, label and file
// version, its macros as the blocks they are here, and the losses.
func TestAnXMLExportIsReadWithItsHistory(t *testing.T) {
	zr := zipOf(t, xmlExport())
	if got := Detect(zr); got != FormatXML {
		t.Fatalf("the export is detected as %q", got)
	}
	sp, err := Read(zr, FormatXML, Options{Key: "ENG", Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	if sp.Name != "Engineering" || sp.Key != "OLD" || sp.Description != "How we build things." {
		t.Errorf("the space reads %q %q %q", sp.Name, sp.Key, sp.Description)
	}
	var order []string
	for _, p := range sp.Pages {
		order = append(order, p.Title)
	}
	if !slices.Equal(order, []string{"Engineering Home", "Build", "Release notes"}) {
		t.Fatalf("the pages are %v", order)
	}
	pages := byTitle(sp)
	home, build, post := pages["Engineering Home"], pages["Build"], pages["Release notes"]
	if home.Parent != nil || *build.Parent != home.ID || !post.Post || post.Parent != nil {
		t.Errorf("the tree or the blog is wrong")
	}
	for _, p := range sp.Pages {
		for _, v := range p.Versions {
			if err := document.Validate(v.Body); err != nil {
				t.Errorf("a version of %s is refused: %v\n%s", p.Title, err, v.Body)
			}
		}
	}
	if len(build.Versions) != 2 || !strings.Contains(string(build.Versions[0].Body), "First draft.") || build.Versions[1].Comment != "Added steps" {
		t.Fatalf("Build's versions are %+v", build.Versions)
	}
	people := map[string]Person{}
	for _, p := range sp.People {
		people[p.Ref] = p
	}
	if people[build.Versions[0].By].Name != "Bob Example" || people[build.Versions[1].By].Email != "ann@example.com" || people[build.CreatedBy].Name != "Bob Example" {
		t.Errorf("the versions are by %+v and %+v", people[build.Versions[0].By], people[build.Versions[1].By])
	}
	if build.CreatedAt.Format(time2) != "2026-02-01" || build.Versions[1].At.Format(time2) != "2026-03-01" {
		t.Errorf("Build's times are %s and %s", build.CreatedAt, build.Versions[1].At)
	}

	if len(build.Files) != 3 || build.Files[0].Name != "diagram.png" || build.Files[1].Name != "diagram.png" || build.Files[2].ContentType != "text/plain" {
		t.Fatalf("Build's files are %+v", build.Files)
	}
	if data, _ := build.Files[0].Read(); string(data) != "first diagram" {
		t.Errorf("the diagram's first version reads %q", data)
	}
	body := current(build)
	for _, want := range []string{
		`{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"go build ./..."}]}`,
		`{"type":"panel","attrs":{"kind":"info"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Heads up","marks":[{"type":"bold"}]}]}`,
		`{"type":"expand","attrs":{"title":"Details"}`,
		`{"type":"status","attrs":{"color":"success","label":"DONE"}}`,
		`{"type":"text","text":" by @Ann Example."}`,
		`{"type":"taskItem","attrs":{"checked":true},"content":[{"type":"paragraph","content":[{"type":"text","text":"Write it"}]}]}`,
		`{"type":"image","attrs":{"alt":null,"attachmentId":"` + build.Files[1].ID.String() + `","width":200}}`,
		`{"type":"attachment","attrs":{"attachmentId":"` + build.Files[2].ID.String() + `","fileName":"notes.txt"}}`,
		`{"type":"columns","content":[{"type":"column"`,
		"Done\u00a0🙂",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Build lacks %s:\n%s", want, body)
		}
	}
	if !strings.Contains(current(home), `"href":"/s/ENG/p/`+build.ID.String()+`"`) || !strings.Contains(current(home), `"childPages"`) {
		t.Errorf("the home page reads %s", current(home))
	}
	if !slices.Equal(build.Labels, []string{"build"}) {
		t.Errorf("Build's labels are %v", build.Labels)
	}
	if len(build.Threads) != 1 || len(build.Threads[0].Comments) != 2 {
		t.Fatalf("Build's discussion is %+v", build.Threads)
	}
	reply := build.Threads[0].Comments[1]
	if _, err := document.ParseComment(reply.Body); err != nil || !strings.Contains(string(reply.Body), "Thanks.") || people[reply.By].Name != "Ann Example" {
		t.Errorf("the reply reads %s by %+v: %v", reply.Body, people[reply.By], err)
	}
	losses := lossKinds(sp)
	if !slices.Contains(losses[LossMacro], "Build: roadmap") || !slices.Contains(losses[LossOutsideLink], "Build: ELSE: Other") {
		t.Errorf("the losses are %v", losses)
	}
}

const time2 = "2006-01-02"

// Lists, quotes and panels nested past what a document holds become their
// words, so a hostile page still makes a page.
func TestNestingPastTheLimitBecomesWords(t *testing.T) {
	deep := strings.Repeat("<blockquote>", 80) + "<p>deep words</p>" + strings.Repeat("</blockquote>", 80) +
		strings.Repeat("<ul><li>", 60) + "listed" + strings.Repeat("</li></ul>", 60)
	root, err := parseHTML([]byte("<html><body>" + deep + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	c := &converter{key: "K", lost: &losses{}}
	body := c.pageDoc(root)
	if err := document.Validate(body); err != nil {
		t.Fatalf("the deep page is refused: %v", err)
	}
	if !strings.Contains(string(body), "deep words") || !strings.Contains(string(body), "listed") {
		t.Errorf("the deep page lost its words: %s", body)
	}
}

// White space reads as a browser shows it, and a comment keeps only what a
// comment may hold.
func TestWhiteSpaceAndCommentsReadAsShown(t *testing.T) {
	root, _ := parseHTML([]byte("<p>  one \n <b> two </b>  three  </p><h4>Deep heading</h4>"))
	c := &converter{key: "K", lost: &losses{}}
	var doc document.Node
	_ = json.Unmarshal(c.pageDoc(root), &doc)
	got := doc.Content[0].Content
	if len(got) != 3 || got[0].Text != "one " || got[1].Text != "two " || got[2].Text != "three" {
		t.Errorf("the paragraph reads %+v", got)
	}
	if doc.Content[1].Type != "heading" || doc.Content[1].Attrs["level"] != float64(3) {
		t.Errorf("an h4 reads as %+v", doc.Content[1])
	}
	root, _ = parseHTML([]byte(`<div class="note"><p>a <img src="x.png" alt="pic"></p></div><ul class="task-list"><li class="checked">done</li></ul>`))
	body := c.commentDoc(root)
	if _, err := document.ParseComment(body); err != nil {
		t.Errorf("the comment is refused: %v\n%s", err, body)
	}
	if !strings.Contains(string(body), `"bulletList"`) || strings.Contains(string(body), "panel") {
		t.Errorf("the comment reads %s", body)
	}
}

// A zip that is neither export is told apart, and an export past the
// limits is refused in a sentence.
func TestDetectionAndLimits(t *testing.T) {
	if got := Detect(zipOf(t, map[string]string{"readme.md": "# hi"})); got != "" {
		t.Errorf("a zip of Markdown is detected as %q", got)
	}
	if _, err := Read(zipOf(t, map[string]string{"index.html": "<html></html>"}), FormatHTML, Options{Key: "K", Limits: limits}); !errors.As(err, new(*InvalidError)) {
		t.Errorf("an index without pages reads as %v", err)
	}
	_, err := Read(zipOf(t, htmlExport()), FormatHTML, Options{Key: "K", Limits: Limits{Pages: 3}})
	var big *TooLargeError
	if !errors.As(err, &big) || !strings.Contains(big.Message, "Split the space") {
		t.Errorf("too many pages read as %v", err)
	}
	_, err = Read(zipOf(t, xmlExport()), FormatXML, Options{Key: "K", Limits: Limits{Files: 1}})
	if !errors.As(err, &big) {
		t.Errorf("too many files read as %v", err)
	}
	if _, err := Read(zipOf(t, map[string]string{"entities.xml": "<not xml"}), FormatXML, Options{Key: "K", Limits: limits}); !errors.As(err, new(*InvalidError)) {
		t.Errorf("broken XML reads as %v", err)
	}
}
