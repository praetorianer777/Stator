package attachment

import (
	"context"
	"testing"
)

type stubConverter struct{}

func (stubConverter) ToPDF(context.Context, string, []byte) ([]byte, error) { return nil, nil }

func TestAFileIsPreviewedByItsTypeOrItsName(t *testing.T) {
	for _, c := range []struct {
		name, contentType string
		want              PreviewKind
		ext               string
	}{
		{"report.pdf", "application/pdf", PreviewPDF, ""},
		{"scan", "application/pdf; name=scan", PreviewPDF, ""},
		{"plan.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", PreviewOffice, "docx"},
		// What a browser that knows nothing of the file sends, sniffed as a zip.
		{"Budget.XLSX", "application/zip", PreviewOffice, "xlsx"},
		{"deck.pptx", "application/octet-stream", PreviewOffice, "pptx"},
		{"notes.odt", "application/vnd.oasis.opendocument.text", PreviewOffice, "odt"},
		{"sheet.ods", "application/zip", PreviewOffice, "ods"},
		{"slides.odp", "application/zip", PreviewOffice, "odp"},
		{"old.doc", "application/octet-stream", PreviewOffice, "doc"},
		{"untitled", "application/vnd.ms-powerpoint", PreviewOffice, "ppt"},
		{"notes.txt", "text/plain; charset=utf-8", PreviewNone, ""},
		{"logo.svg", "image/svg+xml", PreviewNone, ""},
		{"archive.zip", "application/zip", PreviewNone, ""},
		{"docx", "application/zip", PreviewNone, ""},
	} {
		if got := previewKind(c.name, c.contentType); got != c.want {
			t.Errorf("%s (%s) previews as %q, want %q", c.name, c.contentType, got, c.want)
		}
		if got := officeExtension(c.name, c.contentType); got != c.ext {
			t.Errorf("%s (%s) is sent as .%s, want .%s", c.name, c.contentType, got, c.ext)
		}
	}
}

func TestAnOfficeDocumentHasAPreviewOnlyWhereItCanBeConverted(t *testing.T) {
	plan := func(size int64) Attachment {
		return Attachment{FileName: "plan.docx", ContentType: "application/zip", Size: size}
	}
	off := &Service{}
	on := (&Service{}).WithConverter(stubConverter{})
	for _, c := range []struct {
		what string
		s    *Service
		file Attachment
		want PreviewKind
	}{
		{"without a converter", off, plan(1), PreviewNone},
		{"with one", on, plan(1), PreviewOffice},
		{"at the limit", on, plan(MaxConvertSize), PreviewOffice},
		{"over the limit", on, plan(MaxConvertSize + 1), PreviewNone},
		{"a PDF without a converter", off, Attachment{FileName: "a.pdf", ContentType: "application/pdf", Size: MaxConvertSize + 1}, PreviewPDF},
	} {
		file := c.file
		c.s.describe(&file)
		if file.Preview != c.want {
			t.Errorf("%s the file previews as %q, want %q", c.what, file.Preview, c.want)
		}
	}
}

func TestAPreviewIsNamedAsAPDF(t *testing.T) {
	for name, want := range map[string]string{"plan.docx": "plan.pdf", "Q3 budget.xlsx": "Q3 budget.pdf", "untitled": "untitled.pdf"} {
		if got := pdfName(name); got != want {
			t.Errorf("%s is previewed as %s, want %s", name, got, want)
		}
	}
}
