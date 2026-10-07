package attachment

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/config"
)

func TestTheDefaultLimitIsTheConfigured(t *testing.T) {
	if DefaultMaxSize != config.DefaultUploadLimit {
		t.Fatalf("attachment.DefaultMaxSize %d and config.DefaultUploadLimit %d disagree", DefaultMaxSize, config.DefaultUploadLimit)
	}
}

func TestTooLargeNamesTheLimit(t *testing.T) {
	err := error(&TooLargeError{Limit: 50 << 20})
	if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "50 MB") {
		t.Fatalf("got %v", err)
	}
	for n, want := range map[int64]string{1 << 20: "1 MB", 3 << 19: "1.5 MB", 512 << 10: "512 KB", 100: "100 bytes"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestContentTypeTrustsASpecificClaimAndSniffsTheRest(t *testing.T) {
	pngBytes := encoded(t, "png")
	cases := []struct {
		declared string
		data     []byte
		want     string
	}{
		{"application/pdf", []byte("%PDF-1.7"), "application/pdf"},
		{"", pngBytes, "image/png"},
		{"application/octet-stream", pngBytes, "image/png"},
		{"text/html\r\nX-Evil: 1", []byte("hello"), "text/plain; charset=utf-8"},
		{"not a type", []byte("hello"), "text/plain; charset=utf-8"},
	}
	for _, c := range cases {
		if got := contentTypeFor(c.declared, c.data); got != c.want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", c.declared, got, c.want)
		}
	}
}

// webp1x1 is the smallest lossless WebP there is, one pixel.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func encoded(t *testing.T, kind string) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 3, 2), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	var err error
	switch kind {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImagesReportTheirSize(t *testing.T) {
	for _, kind := range []string{"png", "jpeg", "gif"} {
		w, h := dimensions("image/"+kind, encoded(t, kind))
		if w == nil || h == nil || *w != 3 || *h != 2 {
			t.Errorf("%s measured %v x %v, want 3 x 2", kind, w, h)
		}
	}
	webp, _ := base64.StdEncoding.DecodeString(webp1x1)
	if w, h := dimensions("image/webp", webp); w == nil || *w != 1 || *h != 1 {
		t.Errorf("webp measured %v x %v, want 1 x 1", w, h)
	}
	if w, h := dimensions("image/svg+xml", []byte(`<svg width="10" height="10"/>`)); w != nil || h != nil {
		t.Error("an SVG was measured")
	}
	if w, _ := dimensions("image/png", []byte("not a png")); w != nil {
		t.Error("broken bytes were measured")
	}
}

func TestRewriteReferencesPointsCopiesAtTheirOwnFiles(t *testing.T) {
	old1, old2, old3, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	new1, new2, new3 := uuid.New(), uuid.New(), uuid.New()
	body := `{"type":"doc","content":[
		{"type":"image","attrs":{"attachmentId":"` + old1.String() + `","alt":"x","width":480}},
		{"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"attachment","attrs":{"attachmentId":"` + old2.String() + `","fileName":"a.pdf"}}]},
		{"type":"image","attrs":{"attachmentId":"` + other.String() + `","alt":null,"width":null}},
		{"type":"gallery","attrs":{"columns":3},"content":[{"type":"galleryImage","attrs":{"attachmentId":"` + old3.String() + `","caption":"y"}}]},
		{"type":"mention","attrs":{"attachmentId":"` + old1.String() + `"}}]}`
	out, changed, err := RewriteReferences([]byte(body), map[uuid.UUID]uuid.UUID{old1: new1, old2: new2, old3: new3})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	text := string(out)
	if strings.Contains(text, old3.String()) {
		t.Errorf("a gallery's picture still names the original's file: %s", text)
	}
	for _, want := range []string{new1.String(), new2.String(), new3.String(), other.String(), `"width":480`} {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %s", text, want)
		}
	}
	if strings.Count(text, old1.String()) != 1 {
		t.Errorf("only file nodes change, and every one of them: %s", text)
	}
	if !json.Valid(out) {
		t.Fatal("the rewritten body is not JSON")
	}

	same := `{"type":"doc","content":[{"type":"paragraph"}]}`
	out, changed, err = RewriteReferences([]byte(same), map[uuid.UUID]uuid.UUID{old1: new1})
	if err != nil || changed || string(out) != same {
		t.Fatalf("a body without files changed: %s %v %v", out, changed, err)
	}
}
