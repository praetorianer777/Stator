package docx

import (
	"encoding/xml"
	"slices"
	"strings"
	"time"
)

const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsWP  = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	nsA   = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsPic = "http://schemas.openxmlformats.org/drawingml/2006/picture"

	relOffice    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	relCore      = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	relApp       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"
	relStyles    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relNumbering = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	relSettings  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings"
	relHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	relImage     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
)

// The colours a document draws with: the light scheme of the built-in
// theme, since a document is read on white whatever its reader chose.
const (
	ruleColor   = "CBD5E1"
	neutralFill = "F1F5F9"
	accentText  = "1D4ED8"
	successText = "15803D"
	warningText = "B45309"
	quoteBorder = "CBD5E1"
)

// cellFills are a cell's backgrounds, the theme roles the editor offers.
var cellFills = map[string]string{
	"neutral": neutralFill,
	"accent":  "DBEAFE",
	"success": "DCFCE7",
	"warning": "FEF3C7",
	"danger":  "FEE2E2",
}

var panelLooks = map[string]boxStyle{
	"info":    {fill: "EFF6FF", border: "2563EB"},
	"note":    {fill: "F5F3FF", border: "7C3AED"},
	"success": {fill: "F0FDF4", border: "16A34A"},
	"warning": {fill: "FFFBEB", border: "D97706"},
	"error":   {fill: "FEF2F2", border: "DC2626"},
}

type statusLook struct{ text, fill string }

var statusLooks = map[string]statusLook{
	"neutral": {"334155", "E2E8F0"},
	"accent":  {accentText, "DBEAFE"},
	"success": {successText, "DCFCE7"},
	"warning": {warningText, "FEF3C7"},
	"danger":  {"B91C1C", "FEE2E2"},
}

const packageRels = xml.Header + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="` + relOffice + `" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="` + relCore + `" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="` + relApp + `" Target="docProps/app.xml"/>` +
	`</Relationships>`

const appProperties = xml.Header + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Stator</Application></Properties>`

// settings asks Word to read the document as its current version does,
// rather than as a document of an older one.
const settings = xml.Header + `<w:settings xmlns:w="` + nsW + `"><w:defaultTabStop w:val="720"/><w:characterSpacingControl w:val="doNotCompress"/>` +
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`

func contentTypes(pictures []media, branded bool) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	var exts []string
	for _, m := range pictures {
		if !slices.Contains(exts, m.ext) {
			exts = append(exts, m.ext)
		}
	}
	for _, ext := range exts {
		b.WriteString(`<Default Extension="` + ext + `" ContentType="image/` + ext + `"/>`)
	}
	for _, o := range [][2]string{
		{"/word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"},
		{"/word/styles.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"},
		{"/word/numbering.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"},
		{"/word/settings.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"},
		{"/docProps/core.xml", "application/vnd.openxmlformats-package.core-properties+xml"},
		{"/docProps/app.xml", "application/vnd.openxmlformats-officedocument.extended-properties+xml"},
	} {
		b.WriteString(`<Override PartName="` + o[0] + `" ContentType="` + o[1] + `"/>`)
	}
	if branded {
		b.WriteString(`<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>`)
		b.WriteString(`<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>`)
	}
	b.WriteString(`</Types>`)
	return []byte(b.String())
}

// coreProperties are the document's title, author, dates and language, which
// Word shows in its file information and a file manager may list.
func coreProperties(p Page) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`)
	b.WriteString(`<dc:title>` + escape(p.Title) + `</dc:title>`)
	if p.Space != "" {
		b.WriteString(`<dc:subject>` + escape(p.Space) + `</dc:subject>`)
	}
	if p.Author != "" {
		b.WriteString(`<dc:creator>` + escape(p.Author) + `</dc:creator>`)
	}
	if p.Editor != "" {
		b.WriteString(`<cp:lastModifiedBy>` + escape(p.Editor) + `</cp:lastModifiedBy>`)
	}
	if p.Version > 0 {
		b.WriteString(`<cp:revision>` + itoa(p.Version) + `</cp:revision>`)
	}
	b.WriteString(`<dc:language>` + language(p.Language) + `</dc:language>`)
	for _, d := range []struct {
		name string
		at   time.Time
	}{{"created", p.Created}, {"modified", p.Modified}} {
		if !d.at.IsZero() {
			b.WriteString(`<dcterms:` + d.name + ` xsi:type="dcterms:W3CDTF">` + d.at.UTC().Format(time.RFC3339) + `</dcterms:` + d.name + `>`)
		}
	}
	b.WriteString(`</cp:coreProperties>`)
	return []byte(b.String())
}

// Fonts: what Word and the office suites ship, with fallbacks they map.
const (
	bodyFont = "Calibri"
	codeFont = "Consolas"
)

// styles is word/styles.xml. Headings are Word's own, by their built-in
// names, so its navigation pane and its tables of contents find them.
func styles(lang, accent string, branded bool) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<w:styles xmlns:w="` + nsW + `">`)
	b.WriteString(`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="` + bodyFont + `" w:hAnsi="` + bodyFont + `" w:eastAsia="` + bodyFont + `" w:cs="` + bodyFont + `"/>`)
	b.WriteString(`<w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="` + language(lang) + `"/></w:rPr></w:rPrDefault>`)
	b.WriteString(`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`)
	para := func(id, name, based, pPr, rPr string, extra string) {
		b.WriteString(`<w:style w:type="paragraph" w:styleId="` + id + `"><w:name w:val="` + name + `"/>`)
		if based != "" {
			b.WriteString(`<w:basedOn w:val="` + based + `"/><w:next w:val="Normal"/>`)
		}
		b.WriteString(extra)
		if pPr != "" {
			b.WriteString(`<w:pPr>` + pPr + `</w:pPr>`)
		}
		if rPr != "" {
			b.WriteString(`<w:rPr>` + rPr + `</w:rPr>`)
		}
		b.WriteString(`</w:style>`)
	}
	char := func(id, name, rPr string) {
		b.WriteString(`<w:style w:type="character" w:styleId="` + id + `"><w:name w:val="` + name + `"/><w:rPr>` + rPr + `</w:rPr></w:style>`)
	}
	headingColour := "0F172A"
	if branded {
		headingColour = accent
	}
	mono := `<w:rFonts w:ascii="` + codeFont + `" w:hAnsi="` + codeFont + `" w:cs="` + codeFont + `"/>`
	b.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	para("Title", "Title", "Normal", `<w:spacing w:after="80"/>`, `<w:b/><w:color w:val="0F172A"/><w:sz w:val="48"/><w:szCs w:val="48"/>`, `<w:uiPriority w:val="10"/><w:qFormat/>`)
	para("Subtitle", "Subtitle", "Normal", `<w:spacing w:after="240"/>`, `<w:color w:val="64748B"/><w:sz w:val="20"/><w:szCs w:val="20"/>`, `<w:uiPriority w:val="11"/><w:qFormat/>`)
	for level, size := range []string{"32", "28", "24"} {
		n := itoa(level + 1)
		para("Heading"+n, "heading "+n, "Normal",
			`<w:keepNext/><w:keepLines/><w:spacing w:before="`+[]string{"360", "280", "240"}[level]+`" w:after="120"/><w:outlineLvl w:val="`+itoa(level)+`"/>`,
			`<w:b/><w:bCs/><w:color w:val="`+headingColour+`"/><w:sz w:val="`+size+`"/><w:szCs w:val="`+size+`"/>`,
			`<w:uiPriority w:val="9"/><w:qFormat/>`)
	}
	para("Code", "Code", "Normal",
		`<w:pBdr><w:top w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/><w:left w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/><w:bottom w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/><w:right w:val="single" w:sz="4" w:space="4" w:color="E2E8F0"/></w:pBdr><w:shd w:val="clear" w:color="auto" w:fill="F8FAFC"/><w:spacing w:before="120" w:after="200" w:line="240" w:lineRule="auto"/><w:ind w:left="113" w:right="113"/>`,
		mono+`<w:noProof/><w:sz w:val="19"/><w:szCs w:val="19"/>`, `<w:qFormat/>`)
	para("Formula", "Formula", "Normal", `<w:jc w:val="center"/>`, mono+`<w:noProof/><w:color w:val="334155"/>`, `<w:qFormat/>`)
	para("Note", "Note", "Normal", "", `<w:i/><w:iCs/><w:color w:val="64748B"/>`, `<w:qFormat/>`)
	para("Caption", "caption", "Normal", `<w:spacing w:after="200"/>`, `<w:i/><w:iCs/><w:color w:val="64748B"/><w:sz w:val="18"/><w:szCs w:val="18"/>`, `<w:uiPriority w:val="35"/><w:qFormat/>`)
	para("TOCHeading", "TOC Heading", "Normal", `<w:keepNext/><w:spacing w:before="240" w:after="120"/>`, `<w:b/><w:color w:val="0F172A"/><w:sz w:val="24"/><w:szCs w:val="24"/>`, `<w:uiPriority w:val="39"/>`)
	para("Spacer", "Spacer", "Normal", `<w:spacing w:after="0" w:line="160" w:lineRule="exact"/>`, `<w:sz w:val="8"/><w:szCs w:val="8"/>`, `<w:semiHidden/>`)
	for level := 1; level <= 3; level++ {
		n := itoa(level)
		para("TOC"+n, "toc "+n, "Normal", `<w:spacing w:after="60"/><w:ind w:left="`+itoa((level-1)*240)+`"/>`, "", `<w:uiPriority w:val="39"/>`)
	}
	char("Hyperlink", "Hyperlink", `<w:color w:val="`+accent+`"/><w:u w:val="single"/>`)
	char("InlineCode", "Inline Code", mono+`<w:noProof/><w:shd w:val="clear" w:color="auto" w:fill="F1F5F9"/>`)
	char("FormulaChar", "Formula Char", mono+`<w:noProof/><w:color w:val="334155"/>`)
	b.WriteString(`<w:style w:type="table" w:styleId="StatorTable"><w:name w:val="Stator Table"/><w:tblPr><w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + side + ` w:val="single" w:sz="4" w:space="0" w:color="` + ruleColor + `"/>`)
	}
	b.WriteString(`</w:tblBorders><w:tblCellMar><w:top w:w="40" w:type="dxa"/><w:left w:w="` + itoa(cellMargin) + `" w:type="dxa"/><w:bottom w:w="40" w:type="dxa"/><w:right w:w="` + itoa(cellMargin) + `" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	b.WriteString(`<w:style w:type="table" w:styleId="StatorLayout"><w:name w:val="Stator Layout"/><w:tblPr><w:tblCellMar><w:top w:w="80" w:type="dxa"/><w:left w:w="` + itoa(cellMargin) + `" w:type="dxa"/><w:bottom w:w="40" w:type="dxa"/><w:right w:w="` + itoa(cellMargin) + `" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	b.WriteString(`</w:styles>`)
	return []byte(b.String())
}

// The numbering definitions: bullets, and a numbered list in each of the
// counting styles an ordered list may name.
const bulletNum = 1

var orderedFormats = []struct{ kind, format string }{
	{"1", "decimal"}, {"a", "lowerLetter"}, {"A", "upperLetter"}, {"i", "lowerRoman"}, {"I", "upperRoman"},
}

// bullets are the markers of each level, as browsers draw nested lists.
var bullets = []string{"•", "◦", "▪"}

// orderedNum adds a numbering instance for one ordered list, so each list
// counts on its own from where it starts.
func (w *writer) orderedNum(kind string, start int) int {
	abstract := 1
	for i, f := range orderedFormats {
		if f.kind == kind {
			abstract = i + 1
		}
	}
	w.nums = append(w.nums, listNum{abstract: abstract, start: start})
	return bulletNum + len(w.nums)
}

func (w *writer) numbering() []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<w:numbering xmlns:w="` + nsW + `">`)
	level := func(ilvl int, format, text, font string) {
		b.WriteString(`<w:lvl w:ilvl="` + itoa(ilvl) + `"><w:start w:val="1"/><w:numFmt w:val="` + format + `"/><w:lvlText w:val="` + escape(text) + `"/><w:lvlJc w:val="left"/>`)
		b.WriteString(`<w:pPr><w:ind w:left="` + itoa(listIndent*(ilvl+1)) + `" w:hanging="` + itoa(hanging) + `"/></w:pPr>`)
		if font != "" {
			b.WriteString(`<w:rPr><w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `"/></w:rPr>`)
		}
		b.WriteString(`</w:lvl>`)
	}
	b.WriteString(`<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for ilvl := 0; ilvl <= maxListLevel; ilvl++ {
		level(ilvl, "bullet", bullets[ilvl%len(bullets)], bodyFont)
	}
	b.WriteString(`</w:abstractNum>`)
	for i, f := range orderedFormats {
		b.WriteString(`<w:abstractNum w:abstractNumId="` + itoa(i+1) + `"><w:multiLevelType w:val="hybridMultilevel"/>`)
		for ilvl := 0; ilvl <= maxListLevel; ilvl++ {
			level(ilvl, f.format, "%"+itoa(ilvl+1)+".", "")
		}
		b.WriteString(`</w:abstractNum>`)
	}
	b.WriteString(`<w:num w:numId="` + itoa(bulletNum) + `"><w:abstractNumId w:val="0"/></w:num>`)
	for i, n := range w.nums {
		b.WriteString(`<w:num w:numId="` + itoa(bulletNum+i+1) + `"><w:abstractNumId w:val="` + itoa(n.abstract) + `"/>`)
		for ilvl := 0; ilvl <= maxListLevel; ilvl++ {
			b.WriteString(`<w:lvlOverride w:ilvl="` + itoa(ilvl) + `"><w:startOverride w:val="` + itoa(n.start) + `"/></w:lvlOverride>`)
		}
		b.WriteString(`</w:num>`)
	}
	b.WriteString(`</w:numbering>`)
	return []byte(b.String())
}
