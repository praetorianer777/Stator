package convert

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testMaxOutput = 64

// service answers a conversion the way the handler says, after checking the
// document arrived as the one part named files, under its name.
func service(t *testing.T, answer func(w http.ResponseWriter)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != convertPath {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		file, header, err := r.FormFile("files")
		if err != nil {
			t.Errorf("no part named files: %v", err)
			return
		}
		data, _ := io.ReadAll(file)
		if header.Filename != "plan.docx" || string(data) != "document" {
			t.Errorf("sent %q holding %q", header.Filename, data)
		}
		answer(w)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", testMaxOutput)
}

func TestAConvertedDocumentComesBackAsItsPDF(t *testing.T) {
	c := service(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.7 plan"))
	})
	pdf, err := c.ToPDF(context.Background(), "plan.docx", []byte("document"))
	if err != nil || string(pdf) != "%PDF-1.7 plan" {
		t.Fatalf("got %q, %v", pdf, err)
	}
}

func TestARefusalSaysWhoseFaultItIs(t *testing.T) {
	for _, c := range []struct {
		status int
		want   error
	}{
		{http.StatusBadRequest, ErrFailed},
		{http.StatusUnprocessableEntity, ErrFailed},
		{http.StatusRequestEntityTooLarge, ErrFailed},
		{http.StatusServiceUnavailable, ErrUnavailable},
		{http.StatusInternalServerError, ErrUnavailable},
		{http.StatusNotFound, ErrUnavailable},
	} {
		client := service(t, func(w http.ResponseWriter) { http.Error(w, "no", c.status) })
		if _, err := client.ToPDF(context.Background(), "plan.docx", []byte("document")); !errors.Is(err, c.want) {
			t.Errorf("%d reads as %v, want %v", c.status, err, c.want)
		}
	}
}

func TestAnAnswerThatIsNoPDFOrTooLargeIsRefused(t *testing.T) {
	notPDF := service(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>")) })
	if _, err := notPDF.ToPDF(context.Background(), "plan.docx", []byte("document")); !errors.Is(err, ErrFailed) {
		t.Errorf("an HTML answer reads as %v", err)
	}
	large := service(t, func(w http.ResponseWriter) {
		_, _ = w.Write(append([]byte("%PDF-"), make([]byte, testMaxOutput)...))
	})
	if _, err := large.ToPDF(context.Background(), "plan.docx", []byte("document")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("an oversized PDF reads as %v", err)
	}
}

func TestAServiceNobodyAnswersIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	if _, err := New(srv.URL, testMaxOutput).ToPDF(context.Background(), "plan.docx", []byte("document")); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a closed service reads as %v", err)
	}
}
