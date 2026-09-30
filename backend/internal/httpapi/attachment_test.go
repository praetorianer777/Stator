package httpapi

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/attachment"
)

func TestOnlyScriptlessTypesShowInPlace(t *testing.T) {
	for _, ok := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain; charset=utf-8"} {
		if !isSafeInline(ok) {
			t.Errorf("%s does not show in place", ok)
		}
	}
	for _, no := range []string{"image/svg+xml", "text/html", "application/xhtml+xml", "text/xml", "application/javascript", "", "image/png\x00"} {
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
