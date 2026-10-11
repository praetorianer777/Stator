package docx

import (
	"bytes"
	"image"
	"image/png"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Brand is what an organization adds to its documents: its name and logo in
// the header, a line and the page number in the footer, and its colour in
// the headings and links. Empty fields are left out; a nil Brand adds nothing.
type Brand struct {
	Name   string
	Footer string
	// Accent is a colour as six hex digits, with or without a leading #.
	Accent string
	// Logo is a PNG, JPEG, GIF or WebP picture; one Word cannot show is left out.
	Logo []byte
}

const (
	// logoHeightPx is the logo's height in the header, a little under a centimetre.
	logoHeightPx = 36
	headerPart   = "header1.xml"
	footerPart   = "footer1.xml"

	relHeader = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	relFooter = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"

	footerSize = "16"
	headerSize = "20"
	mutedText  = "64748B"
)

var hexColour = regexp.MustCompile(`^#?([0-9A-Fa-f]{6})$`)

// accentOf is a colour Word takes as a run's colour, or the built-in accent.
func accentOf(b *Brand) string {
	if b == nil {
		return accentText
	}
	if m := hexColour.FindStringSubmatch(strings.TrimSpace(b.Accent)); m != nil {
		return strings.ToUpper(m[1])
	}
	return accentText
}

// brandLogo makes a logo ready to embed: PNG and JPEG as they are, WebP as
// PNG, anything else left out.
func brandLogo(data []byte) (out []byte, ext string, width, height int, ok bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", 0, 0, false
	}
	switch format {
	case "png":
		return data, "png", cfg.Width, cfg.Height, true
	case "jpeg":
		return data, "jpeg", cfg.Width, cfg.Height, true
	case "gif", "webp":
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", 0, 0, false
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", 0, 0, false
		}
		return buf.Bytes(), "png", cfg.Width, cfg.Height, true
	}
	return nil, "", 0, 0, false
}

// brandParts are the header and the footer of a branded document, their
// relationships, and the logo they carry. The parts are named for the
// document's relationships, which the caller adds.
type brandParts struct {
	header, footer, headerRels []byte
	logo                       *media
}

func (w *writer) brandParts(b *Brand, accent string) *brandParts {
	if b == nil {
		return nil
	}
	name := strings.TrimSpace(b.Name)
	footer := strings.TrimSpace(b.Footer)
	data, ext, lw, lh, hasLogo := brandLogo(b.Logo)
	if name == "" && footer == "" && !hasLogo {
		return nil
	}
	out := &brandParts{}

	// The header: the logo and the name over a rule in the accent.
	h := &writer{}
	h.b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	h.b.WriteString(`<w:hdr xmlns:w="` + nsW + `" xmlns:r="` + nsR + `" xmlns:wp="` + nsWP + `" xmlns:a="` + nsA + `" xmlns:pic="` + nsPic + `">`)
	h.b.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="4" w:color="` + accent + `"/></w:pBdr><w:spacing w:after="0"/></w:pPr>`)
	if hasLogo {
		out.logo = &media{rid: "rId1", target: "media/brandlogo." + ext, ext: ext, data: data}
		out.headerRels = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + relImage + `" Target="` + out.logo.target + `"/></Relationships>`)
		widthPx := logoHeightPx * lw / lh
		h.drawing(&picture{rid: "rId1", width: lw, height: lh}, widthPx, textWidth/2, name)
	}
	if name != "" {
		if hasLogo {
			h.text("  ", runProps{})
		}
		h.b.WriteString(`<w:r><w:rPr><w:b/><w:bCs/><w:color w:val="` + accent + `"/><w:sz w:val="` + headerSize + `"/><w:szCs w:val="` + headerSize + `"/></w:rPr>`)
		h.b.WriteString(`<w:t xml:space="preserve">` + escape(name) + `</w:t></w:r>`)
	}
	h.b.WriteString(`</w:p></w:hdr>`)
	out.header = []byte(h.b.String())

	// The footer: the line on the left, the page number on the right.
	var f strings.Builder
	f.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	f.WriteString(`<w:ftr xmlns:w="` + nsW + `" xmlns:r="` + nsR + `">`)
	f.WriteString(`<w:p><w:pPr><w:tabs><w:tab w:val="right" w:pos="` + itoa(textWidth) + `"/></w:tabs><w:spacing w:after="0"/></w:pPr>`)
	small := `<w:rPr><w:color w:val="` + mutedText + `"/><w:sz w:val="` + footerSize + `"/><w:szCs w:val="` + footerSize + `"/></w:rPr>`
	if footer != "" && utf8.ValidString(footer) {
		f.WriteString(`<w:r>` + small + `<w:t xml:space="preserve">` + escape(footer) + `</w:t></w:r>`)
	}
	f.WriteString(`<w:r>` + small + `<w:tab/></w:r>`)
	f.WriteString(`<w:fldSimple w:instr=" PAGE "><w:r>` + small + `<w:t>1</w:t></w:r></w:fldSimple>`)
	f.WriteString(`</w:p></w:ftr>`)
	out.footer = []byte(f.String())
	return out
}
