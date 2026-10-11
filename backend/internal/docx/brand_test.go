package docx

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func branded(t *testing.T, b *Brand) opened {
	t.Helper()
	data, err := Bytes(context.Background(), Page{
		ID: uuid.New(), Title: "Guide", Language: "en", Brand: b,
		Body: doc(node("heading", map[string]any{"level": 1}, text("Setup")), para(text("Words."))),
	}, Reader{})
	if err != nil {
		t.Fatal(err)
	}
	return open(t, data)
}

func TestADocumentCarriesTheOrganizationsHeaderAndFooter(t *testing.T) {
	o := branded(t, &Brand{Name: "Acme & Sons", Footer: "Internal use only", Accent: "#c2410c", Logo: pictureBytes(t, 120, 60)})

	header := string(o["word/header1.xml"])
	if !strings.Contains(header, "Acme &amp; Sons") || !strings.Contains(header, "<w:drawing>") || !strings.Contains(header, `w:color w:val="C2410C"`) {
		t.Errorf("the header lacks the name, the logo or the accent:\n%s", header)
	}
	footer := string(o["word/footer1.xml"])
	if !strings.Contains(footer, "Internal use only") || !strings.Contains(footer, `w:instr=" PAGE "`) {
		t.Errorf("the footer lacks the line or the page number:\n%s", footer)
	}
	if rels := string(o["word/_rels/header1.xml.rels"]); !strings.Contains(rels, "media/brandlogo.png") {
		t.Errorf("the header does not point at its logo: %s", rels)
	}
	if len(o["word/media/brandlogo.png"]) == 0 {
		t.Error("the logo is not in the document")
	}
	for _, want := range []string{"header1.xml", "footer1.xml"} {
		if !strings.Contains(string(o["[Content_Types].xml"]), want) {
			t.Errorf("the content types do not name %s", want)
		}
	}
	document := o.doc()
	if !regexp.MustCompile(`<w:sectPr><w:headerReference w:type="default" r:id="rId\d+"/><w:footerReference w:type="default" r:id="rId\d+"/><w:pgSz`).MatchString(document) {
		t.Errorf("the section does not use the header and footer: %s", document[max(0, len(document)-400):])
	}
	styles := string(o["word/styles.xml"])
	if !strings.Contains(styles, `w:styleId="Heading1"`) || strings.Count(styles, `w:val="C2410C"`) < 4 {
		t.Errorf("the headings and links do not take the accent:\n%s", styles)
	}
}

func TestAnOrganizationWithoutALogoGetsItsNameAlone(t *testing.T) {
	o := branded(t, &Brand{Name: "Acme"})
	if header := string(o["word/header1.xml"]); !strings.Contains(header, "Acme") || strings.Contains(header, "<w:drawing>") {
		t.Errorf("a header with the name alone reads:\n%s", header)
	}
	if _, has := o["word/_rels/header1.xml.rels"]; has {
		t.Error("a header without a logo has relationships")
	}
	if !strings.Contains(string(o["word/footer1.xml"]), "PAGE") {
		t.Error("the footer lacks the page number")
	}
}

func TestADocumentWithoutABrandIsAsItWas(t *testing.T) {
	for name, b := range map[string]*Brand{"no brand": nil, "an empty brand": {}} {
		o := branded(t, b)
		for _, part := range []string{"word/header1.xml", "word/footer1.xml"} {
			if _, has := o[part]; has {
				t.Errorf("%s: the document has %s", name, part)
			}
		}
		if strings.Contains(o.doc(), "headerReference") {
			t.Errorf("%s: the section names a header", name)
		}
		if strings.Contains(string(o["word/styles.xml"]), "C2410C") {
			t.Errorf("%s: the styles carry a brand colour", name)
		}
	}
}

func TestALogoWordCannotShowIsLeftOutAndABadColourFallsBack(t *testing.T) {
	o := branded(t, &Brand{Name: "Acme", Accent: "red; drop table", Logo: []byte("not a picture")})
	header := string(o["word/header1.xml"])
	if strings.Contains(header, "<w:drawing>") || !strings.Contains(header, `w:color w:val="`+accentText+`"`) {
		t.Errorf("the header reads:\n%s", header)
	}
}
