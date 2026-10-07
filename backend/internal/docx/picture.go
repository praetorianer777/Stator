package docx

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"strconv"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// picture is a file made ready to embed: its relationship and its size in pixels.
type picture struct {
	rid           string
	width, height int
}

// embed adds a file to the document once, PNG, JPEG and GIF as they are and WebP
// as PNG; nil is a file the reader may not open or no picture Word shows.
func (w *writer) embed(id string) *picture {
	if p, seen := w.embedded[id]; seen {
		return p
	}
	p := w.load(id)
	w.embedded[id] = p
	return p
}

func (w *writer) load(id string) *picture {
	parsed, err := uuid.Parse(id)
	if err != nil || w.read.Picture == nil || w.err != nil {
		return nil
	}
	rc, err := w.read.Picture(w.ctx, parsed)
	if err != nil {
		w.fail(err)
		return nil
	}
	if rc == nil {
		return nil
	}
	defer rc.Close()
	left := MaxBytes - w.spent
	data, err := io.ReadAll(io.LimitReader(rc, left+1))
	if err != nil {
		w.fail(fmt.Errorf("read the picture %s: %w", id, err))
		return nil
	}
	if int64(len(data)) > left {
		w.fail(ErrTooLarge)
		return nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil
	}
	ext := format
	switch format {
	case "png", "gif":
	case "jpeg":
		ext = "jpeg"
	case "webp":
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil
		}
		data, ext = buf.Bytes(), "png"
		if int64(len(data)) > left {
			w.fail(ErrTooLarge)
			return nil
		}
	default:
		return nil
	}
	w.spent += int64(len(data))
	target := "media/picture" + strconv.Itoa(len(w.media)+1) + "." + ext
	rid := w.rel(relImage, target, false)
	w.media = append(w.media, media{rid: rid, target: target, ext: ext, data: data})
	return &picture{rid: rid, width: cfg.Width, height: cfg.Height}
}

// drawing writes a picture inline, widthPx wide or as wide as it is, never
// wider than maxTwips, its description for whoever cannot see it.
func (w *writer) drawing(p *picture, widthPx, maxTwips int, descr string) {
	if widthPx <= 0 {
		widthPx = p.width
	}
	cx := int64(widthPx) * emuPerPixel
	if limit := int64(maxTwips) * emuPerTwip; cx > limit {
		cx = limit
	}
	cy := cx * int64(p.height) / int64(p.width)
	w.pictures++
	n := strconv.Itoa(w.pictures)
	ext := `cx="` + strconv.FormatInt(cx, 10) + `" cy="` + strconv.FormatInt(cy, 10) + `"`
	w.b.WriteString(`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent ` + ext + `/>`)
	w.b.WriteString(`<wp:docPr id="` + n + `" name="Picture ` + n + `" descr="` + escape(descr) + `"/>`)
	w.b.WriteString(`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>`)
	w.b.WriteString(`<a:graphic><a:graphicData uri="` + nsPic + `"><pic:pic><pic:nvPicPr><pic:cNvPr id="` + n + `" name="Picture ` + n + `" descr="` + escape(descr) + `"/><pic:cNvPicPr/></pic:nvPicPr>`)
	w.b.WriteString(`<pic:blipFill><a:blip r:embed="` + p.rid + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`)
	w.b.WriteString(`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext ` + ext + `/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`)
}

func (w *writer) imageBlock(n document.Node, f frame, _ int) {
	alt := stringAttr(n, "alt")
	p := w.embed(stringAttr(n, "attachmentId"))
	if p == nil {
		w.note(f, w.words.pictureMissing(alt))
		return
	}
	w.para(f, paraProps{}, func() { w.drawing(p, intAttr(n, "width", 0), f.width, alt) })
}

// gallery writes its pictures in rows of its columns, each with its caption
// beneath, in a table without borders.
func (w *writer) gallery(n document.Node, f frame, depth int) {
	columns := min(max(intAttr(n, "columns", document.DefaultGalleryColumns), document.MinGalleryColumns), document.MaxGalleryColumns)
	type shown struct {
		p       *picture
		caption string
	}
	var pictures []shown
	missing := 0
	for _, c := range n.Content {
		if p := w.embed(stringAttr(c, "attachmentId")); p != nil {
			pictures = append(pictures, shown{p, stringAttr(c, "caption")})
		} else {
			missing++
		}
	}
	if len(pictures) > 0 {
		columns = min(columns, len(pictures))
		widths := make([]int, columns)
		for i := range widths {
			widths[i] = f.width / columns
		}
		w.tableStart(f, widths, false)
		for start := 0; start < len(pictures); start += columns {
			w.b.WriteString("<w:tr>")
			for i := range columns {
				if start+i >= len(pictures) {
					w.cell(widths[i], 1, "", "", nil, f, depth)
					continue
				}
				s := pictures[start+i]
				w.cell(widths[i], 1, "", "", func(inner frame) {
					w.para(inner, paraProps{align: "center"}, func() { w.drawing(s.p, 0, inner.width, s.caption) })
					if s.caption != "" {
						w.para(inner, paraProps{style: "Caption", align: "center"}, func() { w.text(s.caption, runProps{}) })
					}
				}, f, depth)
			}
			w.b.WriteString("</w:tr>")
		}
		w.tableEnd()
	}
	if missing > 0 && !errors.Is(w.err, ErrTooLarge) {
		w.note(f, w.words.picturesMissing(missing))
	}
}
