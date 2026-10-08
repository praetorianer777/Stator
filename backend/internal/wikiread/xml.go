package wikiread

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// object is one object of the XML export: its class, its id, the values
// and references it holds by name, and the lists of ids it names.
type object struct {
	class string
	id    string
	props map[string]prop
	colls map[string][]string
}

// prop is a value, or a reference to another object by its id.
type prop struct {
	text string
	ref  string
}

func (o *object) text(names ...string) string {
	for _, n := range names {
		if p, ok := o.props[n]; ok && strings.TrimSpace(p.text) != "" {
			return strings.TrimSpace(p.text)
		}
	}
	return ""
}

func (o *object) ref(names ...string) string {
	for _, n := range names {
		if p, ok := o.props[n]; ok && p.ref != "" {
			return p.ref
		}
	}
	return ""
}

func (o *object) number(name string) (int, bool) {
	n, err := strconv.Atoi(o.text(name))
	return n, err == nil
}

func (o *object) when(names ...string) time.Time {
	for _, n := range names {
		if t, ok := parseTime(o.text(n)); ok {
			return t
		}
	}
	return time.Time{}
}

// current says the object is content as it stands, not a draft or deleted.
func (o *object) current() bool {
	s := strings.ToLower(o.text("contentStatus"))
	return s == "" || s == "current"
}

// kept are the classes an import reads; every other object is passed over
// as it is read, so a large export is never held whole.
var kept = map[string]bool{
	"page": true, "blogpost": true, "bodycontent": true, "comment": true, "attachment": true, "label": true,
	"labelling": true, "space": true, "spacedescription": true, "contentproperty": true,
}

func keptClass(class string) bool {
	c := strings.ToLower(class)
	return kept[c] || strings.Contains(c, "user")
}

// parseEntities reads the export's XML document object by object.
func parseEntities(b *bundle) ([]*object, error) {
	f := b.files[entitiesPath]
	if f == nil {
		return nil, invalid("The export lacks entities.xml. Export the space again as XML and import the new zip.")
	}
	if int64(f.UncompressedSize64) > MaxEntitiesBytes {
		return nil, tooLarge("The export's entities.xml unpacks into more than %d MB, more than one import takes. Split the space before you export it.", MaxEntitiesBytes>>20)
	}
	if err := b.count(int64(f.UncompressedSize64)); err != nil {
		return nil, err
	}
	rc, err := f.Open()
	if err != nil {
		return nil, invalid("The export's entities.xml could not be read. Export the space again and import the new zip.")
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, MaxEntitiesBytes))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "utf8") {
			return input, nil
		}
		return nil, fmt.Errorf("the charset %s", charset)
	}
	var (
		out   []*object
		cur   *object
		depth int
		// field is the property or collection being read; inID says its
		// text is the id of what it names.
		field, coll string
		inID        bool
		text        strings.Builder
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, invalid("The export's entities.xml is no XML Stator reads (%v). Export the space again and import the new zip.", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if cur == nil {
				if t.Name.Local == "object" {
					cur = &object{class: attrOf(t, "class"), props: map[string]prop{}, colls: map[string][]string{}}
					depth = 0
				}
				continue
			}
			depth++
			switch {
			case depth == 1 && t.Name.Local == "id":
				inID = true
				text.Reset()
			case depth == 1 && t.Name.Local == "property":
				field = attrOf(t, "name")
				text.Reset()
			case depth == 1 && t.Name.Local == "collection":
				coll = attrOf(t, "name")
			case depth == 2 && field != "" && t.Name.Local == "id":
				inID = true
				text.Reset()
			case depth == 3 && coll != "" && t.Name.Local == "id":
				inID = true
				text.Reset()
			}
		case xml.CharData:
			if cur != nil && (inID || (field != "" && depth == 1)) {
				text.Write(t)
			}
		case xml.EndElement:
			if cur == nil {
				continue
			}
			if depth == 0 {
				if t.Name.Local == "object" && keptClass(cur.class) {
					out = append(out, cur)
				}
				cur = nil
				continue
			}
			switch {
			case depth == 1 && inID:
				cur.id = strings.TrimSpace(text.String())
				inID = false
			case depth == 1 && field != "":
				p := cur.props[field]
				if p.ref == "" {
					p.text = text.String()
				}
				cur.props[field] = p
				field = ""
			case depth == 1 && coll != "":
				coll = ""
			case depth == 2 && inID && field != "":
				cur.props[field] = prop{ref: strings.TrimSpace(text.String())}
				inID = false
			case depth == 3 && inID && coll != "":
				cur.colls[coll] = append(cur.colls[coll], strings.TrimSpace(text.String()))
				inID = false
			}
			depth--
		}
	}
	return out, nil
}

func attrOf(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// xmlUser is somebody the export names, read from whichever objects
// describe them.
type xmlUser struct {
	name, display, email string
}

// xspace is the XML export's objects arranged for reading.
type xspace struct {
	space     *object
	pages     map[string]*object
	history   map[string][]*object
	bodies    map[string]*object
	comments  []*object
	files     []*object
	labels    map[string]*object
	labelling []*object
	props     map[string]map[string]string
	users     map[string]*xmlUser
	byName    map[string]*xmlUser
	desc      map[string]*object
}

func arrangeObjects(objs []*object) *xspace {
	x := &xspace{pages: map[string]*object{}, history: map[string][]*object{}, bodies: map[string]*object{},
		labels: map[string]*object{}, props: map[string]map[string]string{}, users: map[string]*xmlUser{},
		byName: map[string]*xmlUser{}, desc: map[string]*object{}}
	var spaces []*object
	count := map[string]int{}
	for _, o := range objs {
		switch strings.ToLower(o.class) {
		case "page", "blogpost":
			if orig := o.ref("originalVersion"); orig != "" {
				x.history[orig] = append(x.history[orig], o)
			} else if o.current() {
				x.pages[o.id] = o
				count[o.ref("space")]++
			}
		case "bodycontent":
			if c := o.ref("content"); c != "" {
				x.bodies[c] = o
			}
		case "comment":
			x.comments = append(x.comments, o)
		case "attachment":
			x.files = append(x.files, o)
		case "label":
			x.labels[o.id] = o
		case "labelling":
			x.labelling = append(x.labelling, o)
		case "space":
			spaces = append(spaces, o)
		case "spacedescription":
			x.desc[o.id] = o
		case "contentproperty":
			c := o.ref("content")
			if c == "" {
				continue
			}
			if x.props[c] == nil {
				x.props[c] = map[string]string{}
			}
			v := o.text("stringValue")
			if v == "" {
				v = o.text("longValue")
			}
			x.props[c][strings.ToUpper(o.text("name"))] = v
		default:
			x.user(o)
		}
	}
	for _, s := range spaces {
		if x.space == nil || count[s.id] > count[x.space.id] {
			x.space = s
		}
	}
	if x.space != nil && count[x.space.id] > 0 {
		for id, p := range x.pages {
			if s := p.ref("space"); s != "" && s != x.space.id {
				delete(x.pages, id)
			}
		}
	}
	return x
}

// user reads an object that describes somebody: by key, by user name, or both.
func (x *xspace) user(o *object) {
	name := o.text("name", "lowerName", "username")
	u := x.byName[strings.ToLower(name)]
	if u == nil {
		u = &xmlUser{}
	}
	if name != "" {
		u.name = name
		x.byName[strings.ToLower(name)] = u
	}
	if v := o.text("fullName", "displayName"); v != "" {
		u.display = v
	}
	if v := o.text("email", "emailAddress"); v != "" {
		u.email = v
	}
	if o.id != "" {
		if have := x.users[o.id]; have != nil && have != u {
			merge(u, have)
		}
		x.users[o.id] = u
	}
	if key := o.text("key"); key != "" {
		x.users[key] = u
	}
}

func merge(into, from *xmlUser) {
	if into.name == "" {
		into.name = from.name
	}
	if into.display == "" {
		into.display = from.display
	}
	if into.email == "" {
		into.email = from.email
	}
}

// person is somebody an object names in a field, as the export's people
// know them: by key in a reference, or by user name in the older text.
func (x *xspace) person(who *people, o *object, fields ...string) string {
	for _, f := range fields {
		p, ok := o.props[f]
		if !ok {
			continue
		}
		key := p.ref
		if key == "" {
			key = strings.TrimSpace(p.text)
		}
		if key == "" {
			continue
		}
		u := x.users[key]
		if u == nil {
			u = x.byName[strings.ToLower(key)]
		}
		if u == nil {
			return ""
		}
		if u.display == "" || u.email == "" {
			if other := x.byName[strings.ToLower(u.name)]; other != nil && other != u {
				merge(u, other)
			}
		}
		name := u.display
		if name == "" {
			name = u.name
		}
		return who.ref(name, u.email)
	}
	return ""
}

// name is somebody's name as a mention shows it.
func (x *xspace) name(key string) string {
	u := x.users[key]
	if u == nil {
		u = x.byName[strings.ToLower(key)]
	}
	if u == nil {
		return ""
	}
	if u.display == "" {
		if other := x.byName[strings.ToLower(u.name)]; other != nil {
			merge(u, other)
		}
	}
	if u.display != "" {
		return u.display
	}
	return u.name
}

// xfile is one version of a file while the export is read.
type xfile struct {
	obj     *object
	group   string
	version int
}

// readXML reads an XML export: entities.xml with every page, version,
// comment, label and file, and attachments/{page}/{file}/{version} beside it.
func readXML(b *bundle, opts Options) (*Space, error) {
	objs, err := parseEntities(b)
	if err != nil {
		return nil, err
	}
	x := arrangeObjects(objs)
	if x.space == nil && len(x.pages) == 0 {
		return nil, invalid("The export's entities.xml holds no space and no pages. Export the space again as XML and import the new zip.")
	}
	sp := &Space{}
	if x.space != nil {
		sp.Name, sp.Key = x.space.text("name"), x.space.text("key")
		if d := x.desc[x.space.ref("description")]; d != nil {
			if body := x.bodies[d.id]; body != nil {
				if root, err := parseXHTML(body.text("body")); err == nil {
					sp.Description = root.words()
				}
			}
		}
	}
	if opts.Limits.Pages > 0 && len(x.pages)+1 > opts.Limits.Pages {
		return nil, tooLarge("The export holds %d pages, more than the %d one import takes. Split the space before you export it.", len(x.pages), opts.Limits.Pages)
	}
	lost := &losses{}
	who := &people{}

	ids := map[string]*Page{}
	titles := map[string]uuid.UUID{}
	postTitles := map[string]uuid.UUID{}
	versions := 0
	for _, oid := range sortedKeys(x.pages) {
		o := x.pages[oid]
		p := &Page{ID: uuid.Must(uuid.NewV7()), Title: o.text("title"), Post: strings.EqualFold(o.class, "blogpost")}
		if p.Title == "" {
			p.Title = "Page " + oid
		}
		ids[oid] = p
		if p.Post {
			postTitles[p.Title] = p.ID
		} else {
			titles[p.Title] = p.ID
		}
		versions += len(x.history[oid]) + 1
		if opts.Limits.Versions > 0 && versions > opts.Limits.Versions {
			return nil, tooLarge("The export holds more than %d versions, more than one import takes. Split the space before you export it.", opts.Limits.Versions)
		}
	}

	if err := x.readFiles(b, ids, opts, lost); err != nil {
		return nil, err
	}

	for _, oid := range sortedKeys(x.pages) {
		o, p := x.pages[oid], ids[oid]
		named := map[string]*File{}
		for i := range p.Files {
			named[p.Files[i].Name] = &p.Files[i]
		}
		c := &converter{key: opts.Key, page: p.Title, lost: lost, exportKey: sp.Key, person: x.name,
			named:  func(name string) *File { return named[name] },
			titled: func(title string, post bool) (uuid.UUID, bool) { return lookup(titles, postTitles, title, post) },
		}
		hist := x.history[oid]
		sort.SliceStable(hist, func(i, j int) bool {
			a, _ := hist[i].number("version")
			b, _ := hist[j].number("version")
			return a < b
		})
		for _, v := range append(hist, o) {
			by := x.person(who, v, "lastModifier", "lastModifierName", "creator", "creatorName")
			at := v.when("lastModificationDate", "creationDate")
			title := v.text("title")
			if title == "" {
				title = p.Title
			}
			p.Versions = append(p.Versions, Version{Title: title, Body: x.body(c, v.id), Comment: v.text("versionComment"), By: by, At: at})
		}
		p.CreatedBy = x.person(who, o, "creator", "creatorName")
		p.CreatedAt = o.when("creationDate")
		if len(hist) > 0 {
			if first := hist[0].when("creationDate"); !first.IsZero() {
				p.CreatedAt = first
			}
		}
	}

	x.readComments(ids, who, func(p *Page) *converter {
		return &converter{key: opts.Key, page: p.Title, lost: lost, exportKey: sp.Key, person: x.name,
			titled: func(title string, post bool) (uuid.UUID, bool) { return lookup(titles, postTitles, title, post) },
			named: func(name string) *File {
				for i := len(p.Files) - 1; i >= 0; i-- {
					if p.Files[i].Name == name {
						return &p.Files[i]
					}
				}
				return nil
			},
		}
	})
	x.readLabels(ids)

	sp.Pages = x.tree(ids)
	sp.People = who.list
	sp.Losses, sp.LostCount = lost.list, lost.count
	return sp, nil
}

func lookup(pages, posts map[string]uuid.UUID, title string, post bool) (uuid.UUID, bool) {
	if post {
		id, ok := posts[title]
		return id, ok
	}
	id, ok := pages[title]
	return id, ok
}

// body is the document of one version: its body as the export stores it,
// or its words when it is in the older format no element marks.
func (x *xspace) body(c *converter, id string) json.RawMessage {
	o := x.bodies[id]
	if o == nil {
		return plainDoc(nil, 0)
	}
	text := o.text("body")
	if t := o.text("bodyType"); t != "" && t != "2" && !strings.Contains(text, "<") {
		c.lose(LossFormat, "")
		return plainDoc(strings.Split(text, "\n\n"), 1<<20)
	}
	root, err := parseXHTML(text)
	if err != nil {
		c.lose(LossPlainText, "")
		return plainDoc(strings.Split(text, "\n\n"), 1<<20)
	}
	return c.pageDoc(root)
}

// readFiles finds each file's versions and their bytes, the oldest first.
func (x *xspace) readFiles(b *bundle, ids map[string]*Page, opts Options, lost *losses) error {
	groups := map[string][]xfile{}
	for _, o := range x.files {
		if !o.current() && o.ref("originalVersion") == "" {
			continue
		}
		group := o.ref("originalVersion")
		if group == "" {
			group = o.id
		}
		v, _ := o.number("version")
		groups[group] = append(groups[group], xfile{obj: o, group: group, version: v})
	}
	count := 0
	for _, g := range sortedKeys(groups) {
		list := groups[g]
		sort.SliceStable(list, func(i, j int) bool { return list[i].version < list[j].version })
		latest := list[len(list)-1].obj
		for _, f := range list {
			if f.obj.ref("originalVersion") == "" {
				latest = f.obj
			}
		}
		owner := latest.ref("containerContent", "content", "page")
		p := ids[owner]
		if p == nil {
			continue
		}
		for _, f := range list {
			entry := fileEntry(b, owner, g, f)
			title := f.obj.text("title", "fileName")
			if title == "" {
				title = latest.text("title", "fileName")
			}
			if entry == nil || entry.UncompressedSize64 == 0 {
				lost.add(p.Title, LossMissingFile, title)
				continue
			}
			count++
			if opts.Limits.Files > 0 && count > opts.Limits.Files {
				return tooLarge("The export holds more than %d files, more than one import takes. Split the space before you export it.", opts.Limits.Files)
			}
			contentType := f.obj.text("contentType", "mediaType")
			if contentType == "" {
				contentType = x.props[f.obj.id]["MEDIA_TYPE"]
			}
			if contentType == "" {
				contentType = typeOf(title)
			}
			p.Files = append(p.Files, File{
				ID: uuid.Must(uuid.NewV7()), Name: title, ContentType: contentType, Size: int64(entry.UncompressedSize64),
				At: f.obj.when("creationDate", "lastModificationDate"), entry: entry,
			})
		}
	}
	return nil
}

// fileEntry finds a version's bytes: under its page, by the file's first
// id and its version, else by its own id.
func fileEntry(b *bundle, owner, group string, f xfile) *zip.File {
	v := strconv.Itoa(f.version)
	for _, name := range []string{
		path.Join("attachments", owner, group, v), path.Join("attachments", owner, f.obj.id, v),
		path.Join("attachments", owner, f.obj.id), path.Join("attachments", owner, group),
	} {
		if e := b.files[name]; e != nil {
			return e
		}
	}
	return nil
}

// readComments makes each comment without a parent a thread, with the
// replies below it in the order they were written.
func (x *xspace) readComments(ids map[string]*Page, who *people, conv func(*Page) *converter) {
	byID := map[string]*object{}
	for _, o := range x.comments {
		byID[o.id] = o
	}
	root := func(o *object) string {
		id := o.id
		for i := 0; i < 100; i++ {
			parent := byID[byID[id].ref("parent")]
			if parent == nil {
				return id
			}
			id = parent.id
		}
		return id
	}
	threads := map[string][]*object{}
	var order []string
	for _, o := range x.comments {
		if !o.current() {
			continue
		}
		r := root(o)
		if _, ok := threads[r]; !ok {
			order = append(order, r)
		}
		threads[r] = append(threads[r], o)
	}
	sort.SliceStable(order, func(i, j int) bool {
		return byID[order[i]].when("creationDate").Before(byID[order[j]].when("creationDate"))
	})
	for _, r := range order {
		list := threads[r]
		p := ids[byID[r].ref("containerContent", "content", "page", "owner")]
		if p == nil {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].when("creationDate").Before(list[j].when("creationDate")) })
		c := conv(p)
		t := Thread{}
		for _, o := range list {
			body := plainDoc(nil, 0)
			if bc := x.bodies[o.id]; bc != nil {
				if node, err := parseXHTML(bc.text("body")); err == nil {
					body = c.commentDoc(node)
				}
			}
			t.Comments = append(t.Comments, Comment{ID: uuid.Must(uuid.NewV7()), By: x.person(who, o, "creator", "creatorName"), At: o.when("creationDate"), Body: body})
		}
		p.Threads = append(p.Threads, t)
	}
}

// readLabels puts each label on its page; a person's own labels stay theirs.
func (x *xspace) readLabels(ids map[string]*Page) {
	for _, l := range x.labelling {
		label := x.labels[l.ref("label")]
		p := ids[l.ref("content", "owningContent", "labelable")]
		if label == nil || p == nil {
			continue
		}
		switch strings.ToLower(label.text("namespace")) {
		case "", "global", "team":
		default:
			continue
		}
		name := label.text("name")
		if name != "" && !slices.Contains(p.Labels, name) {
			p.Labels = append(p.Labels, name)
		}
	}
}

// tree hangs every page from its parent below the space's home page, in
// the order of their positions, posts after the tree.
func (x *xspace) tree(ids map[string]*Page) []*Page {
	pages := map[string]*hpage{}
	var posts []*Page
	for oid, p := range ids {
		if p.Post {
			posts = append(posts, p)
			continue
		}
		o := x.pages[oid]
		order, ok := o.number("position")
		if !ok {
			order = 1 << 30
		}
		pages[oid] = &hpage{file: oid, page: p, parent: o.ref("parent"), order: order}
	}
	homeID := ""
	if x.space != nil {
		homeID = x.space.ref("homePage")
	}
	home := pages[homeID]
	if home == nil {
		var roots []*hpage
		for _, hp := range pages {
			if pages[hp.parent] == nil {
				roots = append(roots, hp)
			}
		}
		if len(roots) == 1 {
			home = roots[0]
		} else {
			title := "Home"
			if x.space != nil && x.space.text("name") != "" {
				title = x.space.text("name")
			}
			home = &hpage{file: "", page: &Page{ID: uuid.Must(uuid.NewV7()), Title: title, CreatedAt: time.Now()}, order: -1}
			home.page.Versions = []Version{{Title: title, Body: pageWithChildren(nil), At: home.page.CreatedAt}}
			pages[""] = home
		}
	}
	for _, hp := range pages {
		if hp != home && pages[hp.parent] == nil {
			hp.parent = home.file
		}
	}
	out := arrange(home, pages)
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].CreatedAt.Before(posts[j].CreatedAt) })
	return append(out, posts...)
}
