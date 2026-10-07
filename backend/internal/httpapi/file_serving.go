package httpapi

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/attachment"
)

// byteSpan is a stretch of a file: length bytes from start on.
type byteSpan struct{ start, length int64 }

type rangeAnswer int

const (
	wholeFile rangeAnswer = iota
	partOfFile
	outsideFile
)

// wantedRange reads the one range a request asks for. Several ranges, a
// malformed header or a stale If-Range get the whole file, as RFC 9110 allows;
// only a range that starts past the end is refused.
func wantedRange(header, ifRange, etag string, size int64) (byteSpan, rangeAnswer) {
	whole := byteSpan{0, size}
	unit, spec, ok := strings.Cut(strings.TrimSpace(header), "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(unit), "bytes") || strings.Contains(spec, ",") {
		return whole, wholeFile
	}
	if ifRange != "" && ifRange != etag {
		return whole, wholeFile
	}
	first, last, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return whole, wholeFile
	}
	if first == "" {
		suffix, err := strconv.ParseInt(last, 10, 64)
		if err != nil || suffix < 0 {
			return whole, wholeFile
		}
		if suffix == 0 || size == 0 {
			return byteSpan{}, outsideFile
		}
		suffix = min(suffix, size)
		return byteSpan{size - suffix, suffix}, partOfFile
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 {
		return whole, wholeFile
	}
	end := size - 1
	if last != "" {
		if end, err = strconv.ParseInt(last, 10, 64); err != nil || end < start {
			return whole, wholeFile
		}
		end = min(end, size-1)
	}
	if start >= size {
		return byteSpan{}, outsideFile
	}
	return byteSpan{start, end - start + 1}, partOfFile
}

// fileTag names a file's bytes for If-Range. A file's id never names other
// bytes, since a new version is a row of its own.
func fileTag(file *attachment.Located) string {
	return `"` + file.ID.String() + `"`
}

// serveFile answers with a file as a download, or in place when asked and
// its type cannot run script, whole or the one range a player asks for.
func serveFile(w http.ResponseWriter, r *http.Request, file *attachment.Located, cacheControl string) {
	tag := fileTag(file)
	h := w.Header()
	h.Set("Accept-Ranges", "bytes")
	span, answer := wantedRange(r.Header.Get("Range"), r.Header.Get("If-Range"), tag, file.Size)
	if answer == outsideFile {
		h.Set("Content-Range", fmt.Sprintf("bytes */%d", file.Size))
		respondError(w, r, errRangeOutside(file.Size))
		return
	}
	var (
		body io.ReadCloser
		err  error
	)
	if answer == partOfFile {
		body, err = file.Range(r.Context(), span.start, span.length)
	} else {
		body, err = file.Bytes(r.Context())
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	defer body.Close()

	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && isSafeInline(file.ContentType) {
		disposition = "inline"
	}
	h.Set("Content-Type", file.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.FileName}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", tag)
	if cacheControl != "" {
		h.Set("Cache-Control", cacheControl)
	}
	h.Set("Content-Length", strconv.FormatInt(span.length, 10))
	status := http.StatusOK
	if answer == partOfFile {
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", span.start, span.start+span.length-1, file.Size))
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, body)
}

func errRangeOutside(size int64) *APIError {
	return &APIError{Status: http.StatusRequestedRangeNotSatisfiable, Code: "range_not_satisfiable",
		Message: fmt.Sprintf("The range asked for starts past the end of the file, which has %d bytes. Ask for a range inside it, or for the whole file.", size)}
}
