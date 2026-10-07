package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/attachment"
)

func TestOnlyScriptlessTypesShowInPlace(t *testing.T) {
	for _, ok := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4", "video/webm", "video/ogg", "application/pdf", "text/plain; charset=utf-8"} {
		if !isSafeInline(ok) {
			t.Errorf("%s does not show in place", ok)
		}
	}
	for _, no := range []string{"image/svg+xml", "video/quicktime", "audio/mpeg", "text/html", "application/xhtml+xml", "text/xml", "application/javascript", "", "image/png\x00"} {
		if isSafeInline(no) {
			t.Errorf("%q shows in place", no)
		}
	}
}

func multipartBody(t *testing.T, field string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile(field, "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	return &body, form.FormDataContentType()
}

func TestAnUploadOverTheLimitIsRefusedAtTheConnection(t *testing.T) {
	const limit = 1 << 10
	body, contentType := multipartBody(t, "other", make([]byte, limit+uploadSlack+1))
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	_, err := readUploadedFile(httptest.NewRecorder(), req, limit)
	var tooLarge *attachment.TooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != limit {
		t.Fatalf("got %v, want the size refusal naming the limit", err)
	}
	if got := toAPIError(err); got.Status != http.StatusRequestEntityTooLarge || got.Code != "too_large" || got.Message != "That file is too large: files on a page are up to 1 KB; make it smaller or split it." {
		t.Fatalf("answered %d %s %q", got.Status, got.Code, got.Message)
	}
}

func TestAnUploadNeedsAPartNamedFile(t *testing.T) {
	body, contentType := multipartBody(t, "other", []byte("x"))
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set("Content-Type", contentType)
	if _, err := readUploadedFile(httptest.NewRecorder(), req, 1<<10); toAPIError(err).Status != http.StatusBadRequest {
		t.Fatalf("got %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	if _, err := readUploadedFile(httptest.NewRecorder(), req, 1<<10); toAPIError(err).Status != http.StatusBadRequest {
		t.Fatalf("got %v", err)
	}
	if got := toAPIError(attachment.ErrEmpty); got.Status != http.StatusUnprocessableEntity || got.Fields["file"] == "" {
		t.Fatalf("an empty file answers %d %v", got.Status, got.Fields)
	}
}

// Every way a preview can be missing reads as a sentence that says to
// download the file, under a code of its own the client can tell apart.
func TestRestoringTheLatestVersionSaysWhichToRestore(t *testing.T) {
	got := toAPIError(fmt.Errorf("restore: %w", &attachment.AlreadyLatestError{Name: "budget.csv", Version: 3}))
	want := "Version 3 of budget.csv is already the latest; restore an earlier version instead."
	if got.Status != http.StatusConflict || got.Code != "already_latest" || got.Message != want {
		t.Errorf("answered %d %s %q, want 409 already_latest %q", got.Status, got.Code, got.Message, want)
	}
}

func TestAMissingPreviewSaysToDownloadTheFile(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{attachment.ErrNoPreview, http.StatusUnsupportedMediaType, "no_preview"},
		{attachment.ErrPreviewTooLarge, http.StatusRequestEntityTooLarge, "preview_too_large"},
		{attachment.ErrPreviewFailed, http.StatusUnprocessableEntity, "preview_failed"},
		{attachment.ErrPreviewOff, http.StatusServiceUnavailable, "preview_off"},
		{fmt.Errorf("%w: refused", attachment.ErrConverterUnavailable), http.StatusServiceUnavailable, "preview_unavailable"},
	} {
		got := toAPIError(c.err)
		if got.Status != c.status || got.Code != c.code || !strings.Contains(strings.ToLower(got.Message), "download") || !strings.HasSuffix(got.Message, ".") {
			t.Errorf("%v answered %d %s %q", c.err, got.Status, got.Code, got.Message)
		}
	}
	if got := toAPIError(attachment.ErrPreviewTooLarge).Message; !strings.Contains(got, "20 MB") {
		t.Errorf("the size refusal does not name the limit: %q", got)
	}
}

func TestARangeIsOneStretchInsideTheFileOrTheWholeFile(t *testing.T) {
	const size = 100
	tag := `"f"`
	for _, c := range []struct {
		header, ifRange string
		span            byteSpan
		answer          rangeAnswer
	}{
		{"", "", byteSpan{0, size}, wholeFile},
		{"bytes=0-9", "", byteSpan{0, 10}, partOfFile},
		{"bytes=90-", "", byteSpan{90, 10}, partOfFile},
		{"bytes=95-500", "", byteSpan{95, 5}, partOfFile},
		{"bytes=-20", "", byteSpan{80, 20}, partOfFile},
		{"bytes=-500", "", byteSpan{0, size}, partOfFile},
		{"Bytes = 5-5", "", byteSpan{5, 1}, partOfFile},
		{"bytes=0-9", tag, byteSpan{0, 10}, partOfFile},
		{"bytes=0-9", `"other"`, byteSpan{0, size}, wholeFile},
		{"bytes=0-1,5-6", "", byteSpan{0, size}, wholeFile},
		{"bytes=9-1", "", byteSpan{0, size}, wholeFile},
		{"bytes=x-1", "", byteSpan{0, size}, wholeFile},
		{"bytes=--1", "", byteSpan{0, size}, wholeFile},
		{"items=0-1", "", byteSpan{0, size}, wholeFile},
		{"bytes=5", "", byteSpan{0, size}, wholeFile},
		{"bytes=100-", "", byteSpan{}, outsideFile},
		{"bytes=100-200", "", byteSpan{}, outsideFile},
		{"bytes=-0", "", byteSpan{}, outsideFile},
	} {
		span, answer := wantedRange(c.header, c.ifRange, tag, size)
		if span != c.span || answer != c.answer {
			t.Errorf("Range %q If-Range %q: got %v %d, want %v %d", c.header, c.ifRange, span, answer, c.span, c.answer)
		}
	}
}

func TestARangeStartingPastTheEndSaysWhatToAskFor(t *testing.T) {
	got := toAPIError(errRangeOutside(42))
	if got.Status != http.StatusRequestedRangeNotSatisfiable || got.Code != "range_not_satisfiable" || !strings.Contains(got.Message, "42 bytes") {
		t.Fatalf("answered %d %s %q", got.Status, got.Code, got.Message)
	}
}
