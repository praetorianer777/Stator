package wikiread

import (
	"mime"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// hpage is one page file of the HTML export while it is read.
type hpage struct {
	file   string
	page   *Page
	parent string
	// order is the page's place in the index's tree, or after it.
	order int
	// extra are files outside the attachments folder its pictures show.
	extra map[string]*File
}

// people names everybody an export mentions once, by address when it has
// one and else by name.
type people struct {
	refs map[string]string
	list []Person
}

func (p *people) ref(name, email string) string {
	name, email = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(email))
	key := "email:" + email
	if email == "" {
		if name == "" {
			return ""
		}
		key = "name:" + strings.ToLower(name)
	}
	if p.refs == nil {
		p.refs = map[string]string{}
	}
	if ref, ok := p.refs[key]; ok {
		return ref
	}
	p.refs[key] = key
	p.list = append(p.list, Person{Ref: key, Name: name, Email: email})
	return key
}

var trailingID = regexp.MustCompile(`(?:^|_)(\d+)$`)

// readHTML reads an HTML export: an index.html holding the page tree as
// nested lists, a page file each, and attachments/{page id}/ beside them.
func readHTML(b *bundle, opts Options) (*Space, error) {
	data, err := b.read(indexPath, MaxPageBytes)
	if err != nil {
		return nil, err
	}
	index, err := parseHTML(data)
	if err != nil {
		return nil, invalid("The export's index.html is no HTML Stator reads. Export the space again and import the new zip.")
	}
	sp := &Space{Name: titleOf(index)}
	lost := &losses{}
	who := &people{}

	pages := map[string]*hpage{}
	var names []string
	for _, name := range b.names {
		if !strings.Contains(name, "/") && strings.HasSuffix(strings.ToLower(name), ".html") && name != indexPath {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, invalid("The export holds no page beside its index.html. Export the space again with its pages and import the new zip.")
	}
	if opts.Limits.Pages > 0 && len(names)+1 > opts.Limits.Pages {
		return nil, tooLarge("The export holds %d pages, more than the %d one import takes. Split the space before you export it.", len(names), opts.Limits.Pages)
	}
	for _, name := range names {
		pages[name] = &hpage{file: name, page: &Page{ID: uuid.Must(uuid.NewV7())}, order: len(names), extra: map[string]*File{}}
	}

	byDir := map[string][]string{}
	for _, name := range b.names {
		if rest, ok := strings.CutPrefix(name, attachDir); ok {
			if dir, _, ok := strings.Cut(rest, "/"); ok && b.files[name].UncompressedSize64 > 0 {
				byDir[dir] = append(byDir[dir], name)
			}
		}
	}
	files := map[string]*File{}
	count := 0
	for _, name := range names {
		hp := pages[name]
		stem := strings.TrimSuffix(name, path.Ext(name))
		dirs := []string{stem}
		if m := trailingID.FindStringSubmatch(stem); m != nil && m[1] != stem {
			dirs = append(dirs, m[1])
		}
		for _, dir := range dirs {
			for _, fname := range byDir[dir] {
				if files[fname] != nil {
					continue
				}
				count++
				if opts.Limits.Files > 0 && count > opts.Limits.Files {
					return nil, tooLarge("The export holds more than %d files, more than one import takes. Split the space before you export it.", opts.Limits.Files)
				}
				f := newFile(b, fname)
				files[fname] = f
				hp.page.Files = append(hp.page.Files, *f)
			}
		}
	}

	// The tree first, so a page's parent and place are known before any
	// page is read; links between pages need only their files' names.
	treeList := bestList(index, pages)
	order := 0
	if treeList != nil {
		var walk func(list *node, parent string)
		walk = func(list *node, parent string) {
			for _, li := range list.kids {
				if li.tag != "li" {
					continue
				}
				here := parent
				link := li.find(func(n *node) bool {
					return n.tag == "a" && pages[localPath(n.attr("href"))] != nil && !n.within(func(up *node) bool {
						return up != li && (up.tag == "ul" || up.tag == "ol") && up.within(func(x *node) bool { return x == li })
					})
				})
				if link != nil {
					hp := pages[localPath(link.attr("href"))]
					if hp.order >= len(names) {
						hp.order = order
						order++
						hp.parent = parent
						if parent == "" {
							hp.parent = indexPath
						}
					}
					here = hp.file
				}
				for _, sub := range li.findAll(func(n *node) bool { return n.tag == "ul" || n.tag == "ol" }) {
					walk(sub, here)
				}
			}
		}
		walk(treeList, "")
	}

	var current *hpage
	link := func(raw string) target {
		p := localPath(raw)
		switch {
		case p == "":
			return target{}
		case p == indexPath:
			return target{href: "/s/" + opts.Key}
		case pages[p] != nil:
			return target{href: "/s/" + opts.Key + "/p/" + pages[p].page.ID.String()}
		case files[p] != nil:
			return target{file: files[p]}
		}
		if current == nil || b.files[p] == nil || b.files[p].UncompressedSize64 == 0 || strings.HasSuffix(strings.ToLower(p), ".html") {
			return target{}
		}
		if f := current.extra[p]; f != nil {
			return target{file: f}
		}
		count++
		if opts.Limits.Files > 0 && count > opts.Limits.Files {
			return target{}
		}
		f := newFile(b, p)
		current.extra[p] = f
		return target{file: f}
	}

	for _, name := range names {
		hp := pages[name]
		current = hp
		if err := readHTMLPage(b, hp, sp.Name, opts, link, lost, who); err != nil {
			return nil, err
		}
		for _, p := range sortedKeys(hp.extra) {
			hp.page.Files = append(hp.page.Files, *hp.extra[p])
		}
	}
	current = nil

	home := homeOf(index, treeList, pages, sp, opts, link, lost)
	sp.Pages = arrange(home, pages)
	sp.People = who.list
	sp.Losses, sp.LostCount = lost.list, lost.count
	return sp, nil
}

func newFile(b *bundle, name string) *File {
	f := b.files[name]
	base := path.Base(name)
	return &File{
		ID: uuid.Must(uuid.NewV7()), Name: base, ContentType: typeOf(base), Size: int64(f.UncompressedSize64),
		At: f.Modified, entry: f,
	}
}

// typeOf is a file's type by its name, empty when its name says none; only
// text keeps its parameters, which name its character set.
func typeOf(name string) string {
	t := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	media, _, err := mime.ParseMediaType(t)
	switch {
	case t == "" || err != nil:
		return ""
	case strings.HasPrefix(media, "text/"):
		return t
	}
	return media
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// localPath is a link within the export as the path of the file it names,
// or nothing for a link elsewhere.
func localPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "/") || isWeb(raw) {
		return ""
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if strings.Contains(strings.SplitN(raw, "/", 2)[0], ":") {
		return ""
	}
	if un, err := url.PathUnescape(raw); err == nil {
		raw = un
	}
	p := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if p == "." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// titleOf is a document's title as its head names it.
func titleOf(doc *node) string {
	if t := doc.find(byTag("title")); t != nil {
		if words := t.words(); words != "" {
			return words
		}
	}
	if h := doc.find(byTag("h1")); h != nil {
		return h.words()
	}
	return ""
}

// bestList is the index's list that links the most pages: its page tree.
func bestList(index *node, pages map[string]*hpage) *node {
	var (
		best  *node
		links int
	)
	for _, list := range index.findAll(func(n *node) bool { return n.tag == "ul" || n.tag == "ol" }) {
		n := len(list.findAll(func(a *node) bool { return a.tag == "a" && pages[localPath(a.attr("href"))] != nil }))
		if n > links {
			best, links = list, n
		}
	}
	return best
}

// readHTMLPage reads one page file: its title, where it hangs, who wrote it
// when, its labels, comments and content.
func readHTMLPage(b *bundle, hp *hpage, spaceName string, opts Options, link func(string) target, lost *losses, who *people) error {
	entry := b.files[hp.file]
	stem := strings.TrimSuffix(hp.file, path.Ext(hp.file))
	hp.page.Title = stem
	if int64(entry.UncompressedSize64) > MaxPageBytes {
		lost.add(stem, LossUnreadPage, hp.file)
		hp.page.Versions = []Version{{Title: stem, Body: plainDoc(nil, 0), At: entry.Modified}}
		return nil
	}
	data, err := b.read(hp.file, MaxPageBytes)
	if err != nil {
		return err
	}
	doc, err := parseHTML(data)
	if err != nil {
		lost.add(stem, LossUnreadPage, hp.file)
		hp.page.Versions = []Version{{Title: stem, Body: plainDoc(nil, 0), At: entry.Modified}}
		return nil
	}
	content := contentOf(doc)
	title, titleNode := pageTitleOf(doc, content, spaceName)
	if title != "" {
		hp.page.Title = title
	}
	p := hp.page

	if hp.parent == "" {
		hp.parent = crumbParent(doc, hp.file)
	}

	created, updated := entry.Modified, entry.Modified
	var by, lastBy string
	if meta := doc.find(func(n *node) bool { return n.attr("id") == "page-metadata" || n.hasClass("page-metadata") }); meta != nil {
		authors := meta.findAll(func(n *node) bool { return n.hasClass("author") || n.hasClass("editor") })
		if len(authors) > 0 {
			by = who.ref(personOf(authors[0]))
			lastBy = who.ref(personOf(authors[len(authors)-1]))
		}
		if dates := datesIn(meta); len(dates) > 0 {
			created, updated = dates[0], dates[len(dates)-1]
		}
		meta.detach()
	}
	p.CreatedBy, p.CreatedAt = by, created

	if box := doc.find(func(n *node) bool {
		return n.attr("id") == "labels" || n.attr("id") == "labels-section" || n.hasClass("labels") || n.hasClass("label-list")
	}); box != nil {
		for _, l := range box.findAll(func(n *node) bool { return n.tag == "a" || n.tag == "li" || n.hasClass("label") }) {
			name := strings.TrimPrefix(l.words(), "#")
			if name != "" && !slices.Contains(p.Labels, name) {
				p.Labels = append(p.Labels, name)
			}
		}
		box.detach()
	}

	nameFiles(doc, p, link)

	c := &converter{key: opts.Key, page: p.Title, lost: lost, link: link}
	if box := doc.find(func(n *node) bool {
		return n.attr("id") == "comments-section" || n.attr("id") == "comments" || n.hasClass("comments") || n.hasClass("comments-section")
	}); box != nil {
		for _, top := range box.findAll(isComment) {
			t := Thread{}
			readComment(top, &t, c, who)
			sort.SliceStable(t.Comments, func(i, j int) bool { return t.Comments[i].At.Before(t.Comments[j].At) })
			if len(t.Comments) > 0 {
				p.Threads = append(p.Threads, t)
			}
		}
		box.detach()
	}

	if titleNode == nil {
		// Without a title marked as such, a page that opens with its title
		// as a heading would show it twice.
		if h := content.find(byTag("h1")); h != nil && h.words() == p.Title {
			titleNode = h
		}
	}
	if titleNode != nil {
		titleNode.detach()
	}
	for _, gone := range content.findAll(func(n *node) bool {
		id := n.attr("id")
		return n.tag == "nav" || n.tag == "header" || n.tag == "footer" || id == "breadcrumbs" || id == "breadcrumb-section" ||
			id == "footer" || id == "attachments" || n.hasClass("breadcrumbs") || n.hasClass("attachments") || n.hasClass("page-attachments")
	}) {
		gone.detach()
	}
	body := c.pageDoc(content)
	p.Versions = []Version{{Title: p.Title, Body: body, By: lastBy, At: updated}}
	if lastBy == "" {
		p.Versions[0].By = by
	}
	return nil
}

func isComment(n *node) bool { return n.hasClass("comment") }

// readComment reads one comment into its thread, then the replies inside it.
func readComment(cm *node, t *Thread, c *converter, who *people) {
	own := func(n *node) bool {
		return !n.within(func(up *node) bool {
			return up != cm && isComment(up) && up.within(func(x *node) bool { return x == cm })
		})
	}
	replies := cm.findAll(isComment)
	for _, r := range replies {
		r.detach()
	}
	var by string
	if a := cm.find(func(n *node) bool { return (n.hasClass("author") || n.hasClass("comment-author")) && own(n) }); a != nil {
		by = who.ref(personOf(a))
		a.detach()
	}
	at := time.Time{}
	if d := cm.find(func(n *node) bool {
		return (n.tag == "time" || n.hasClass("date") || n.hasClass("comment-date")) && own(n)
	}); d != nil {
		if dates := datesIn(d); len(dates) > 0 {
			at = dates[0]
		}
		d.detach()
	}
	body := cm.find(func(n *node) bool { return n.hasClass("comment-body") || n.hasClass("comment-content") })
	if body == nil {
		body = cm
	}
	if strings.TrimSpace(body.words()) != "" || body.find(byTag("img")) != nil {
		t.Comments = append(t.Comments, Comment{ID: uuid.Must(uuid.NewV7()), By: by, At: at, Body: c.commentDoc(body)})
	}
	for _, r := range replies {
		readComment(r, t, c, who)
	}
}

// personOf reads somebody named in a page: their name, and their address
// when a mail link carries it.
func personOf(n *node) (string, string) {
	email := n.attr("data-email")
	if m := n.find(func(a *node) bool {
		return a.tag == "a" && strings.HasPrefix(strings.ToLower(a.attr("href")), "mailto:")
	}); m != nil {
		email = strings.TrimPrefix(strings.TrimPrefix(m.attr("href"), "mailto:"), "MAILTO:")
	} else if strings.HasPrefix(strings.ToLower(n.attr("href")), "mailto:") {
		email = n.attr("href")[len("mailto:"):]
	}
	if i := strings.IndexByte(email, '?'); i >= 0 {
		email = email[:i]
	}
	return n.words(), email
}

// contentOf is the element holding the page's content, else its body.
func contentOf(doc *node) *node {
	if n := doc.find(byID("main-content")); n != nil {
		return n
	}
	for _, class := range []string{"wiki-content", "page-content", "main-content"} {
		if n := doc.find(byClass(class)); n != nil {
			return n
		}
	}
	for _, tag := range []string{"main", "article", "body"} {
		if n := doc.find(byTag(tag)); n != nil {
			return n
		}
	}
	return doc
}

// pageTitleOf is the page's title: an element marked as it, a level 1
// heading outside the content, or the document's title less the space's name.
func pageTitleOf(doc, content *node, spaceName string) (string, *node) {
	if n := doc.find(func(n *node) bool {
		return n.attr("id") == "title-text" || n.hasClass("title-text") || n.hasClass("page-title")
	}); n != nil {
		return strip(n.words(), spaceName), n
	}
	if n := doc.find(func(n *node) bool {
		return n.tag == "h1" && n != content && !n.within(func(up *node) bool { return up == content })
	}); n != nil {
		return strip(n.words(), spaceName), n
	}
	if t := doc.find(byTag("title")); t != nil {
		return strip(t.words(), spaceName), nil
	}
	return "", nil
}

func strip(title, spaceName string) string {
	for _, sep := range []string{" : ", ": ", " - "} {
		if spaceName != "" && strings.HasPrefix(title, spaceName+sep) {
			return strings.TrimSpace(strings.TrimPrefix(title, spaceName+sep))
		}
	}
	return strings.TrimSpace(title)
}

// crumbParent is the page the breadcrumbs name last before this one.
func crumbParent(doc *node, self string) string {
	crumbs := doc.find(func(n *node) bool {
		return n.attr("id") == "breadcrumbs" || n.hasClass("breadcrumbs") || n.hasClass("breadcrumb")
	})
	if crumbs == nil {
		return ""
	}
	parent := ""
	for _, a := range crumbs.findAll(byTag("a")) {
		p := localPath(a.attr("href"))
		if p != "" && p != self && strings.HasSuffix(strings.ToLower(p), ".html") {
			parent = p
		}
	}
	crumbs.detach()
	return parent
}

// nameFiles gives a file the name a link to it shows, as an export that
// names files by their id still lists them by name.
func nameFiles(doc *node, p *Page, link func(string) target) {
	for _, a := range doc.findAll(byTag("a")) {
		t := link(a.attr("href"))
		if t.file == nil {
			continue
		}
		words := a.words()
		if words == "" || !strings.Contains(words, ".") || strings.ContainsAny(words, "/\\") {
			continue
		}
		for i := range p.Files {
			if p.Files[i].ID == t.file.ID && p.Files[i].Name == path.Base(p.Files[i].entry.Name) {
				p.Files[i].Name = words
				if p.Files[i].ContentType == "" {
					p.Files[i].ContentType = typeOf(words)
				}
				t.file.Name, t.file.ContentType = p.Files[i].Name, p.Files[i].ContentType
			}
		}
	}
}

var (
	isoDate  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2})?)?`)
	wordDate = regexp.MustCompile(`(?:[A-Z][a-z]{2,8} \d{1,2}, \d{4})|(?:\d{1,2} [A-Z][a-z]{2,8},? \d{4})|(?:\d{1,2}\.\d{1,2}\.\d{4})`)
)

var wordLayouts = []string{"Jan 2, 2006", "January 2, 2006", "2 Jan 2006", "2 January 2006", "2 Jan, 2006", "2.1.2006"}

// datesIn are the dates an element states, in the order it states them: in
// time elements first, else in its words.
func datesIn(n *node) []time.Time {
	var out []time.Time
	stamps := n.findAll(byTag("time"))
	if n.tag == "time" {
		stamps = append([]*node{n}, stamps...)
	}
	for _, t := range stamps {
		if at, ok := parseTime(t.attr("datetime")); ok {
			out = append(out, at)
		}
	}
	if len(out) > 0 {
		return out
	}
	words := n.words()
	for _, m := range isoDate.FindAllString(words, -1) {
		if at, ok := parseTime(m); ok {
			out = append(out, at)
		}
	}
	for _, m := range wordDate.FindAllString(words, -1) {
		for _, l := range wordLayouts {
			if at, err := time.Parse(l, m); err == nil {
				out = append(out, at)
				break
			}
		}
	}
	return out
}

var timeLayouts = []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", time.DateOnly,
	"2006-01-02 15:04:05.000", "2006-01-02 15:04:05.0"}

func parseTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// homeOf is the space's home page: the one root of the index's tree, or
// else a page made of the index itself, above every root.
func homeOf(index, treeList *node, pages map[string]*hpage, sp *Space, opts Options, link func(string) target, lost *losses) *hpage {
	var roots []*hpage
	for _, hp := range pages {
		if hp.parent == indexPath {
			roots = append(roots, hp)
		}
	}
	if len(roots) == 1 {
		root := roots[0]
		root.parent = ""
		for _, hp := range pages {
			if hp != root && (hp.parent == "" || hp.parent == indexPath || pages[hp.parent] == nil) {
				hp.parent = root.file
			}
		}
		return root
	}
	if treeList != nil {
		treeList.detach()
	}
	title := sp.Name
	if title == "" {
		title = "Home"
	}
	c := &converter{key: opts.Key, page: title, lost: lost, link: link}
	content := contentOf(index)
	if t := content.find(byTag("h1")); t != nil && t.words() == sp.Name {
		t.detach()
	}
	body := c.blocks(content.kids)
	home := &hpage{file: indexPath, page: &Page{ID: uuid.Must(uuid.NewV7()), Title: title}, order: -1}
	doc := pageWithChildren(body)
	at := time.Now()
	for _, hp := range pages {
		if !hp.page.CreatedAt.IsZero() && hp.page.CreatedAt.Before(at) {
			at = hp.page.CreatedAt
		}
		if hp.parent == "" || hp.parent == indexPath || pages[hp.parent] == nil {
			hp.parent = indexPath
		}
	}
	home.page.CreatedAt = at
	home.page.Versions = []Version{{Title: title, Body: doc, At: at}}
	pages[indexPath] = home
	return home
}

// arrange lists the pages home first, each after its parent, siblings in
// the tree's order then by title; parents in a circle hang from the home page.
func arrange(home *hpage, pages map[string]*hpage) []*Page {
	children := map[string][]*hpage{}
	for _, hp := range pages {
		if hp != home {
			children[hp.parent] = append(children[hp.parent], hp)
		}
	}
	for _, list := range children {
		sort.Slice(list, func(i, j int) bool {
			if list[i].order != list[j].order {
				return list[i].order < list[j].order
			}
			return list[i].page.Title < list[j].page.Title
		})
	}
	var out []*Page
	seen := map[string]bool{}
	var visit func(hp *hpage, parent *uuid.UUID)
	visit = func(hp *hpage, parent *uuid.UUID) {
		if seen[hp.file] {
			return
		}
		seen[hp.file] = true
		hp.page.Parent = parent
		out = append(out, hp.page)
		id := hp.page.ID
		for _, k := range children[hp.file] {
			visit(k, &id)
		}
	}
	visit(home, nil)
	homeID := home.page.ID
	for _, name := range sortedKeys(pages) {
		if hp := pages[name]; !seen[name] {
			hp.parent = home.file
			visit(hp, &homeID)
		}
	}
	return out
}
