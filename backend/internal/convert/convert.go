// Package convert turns office documents into PDF for previews, through a
// headless office suite that runs as a service of its own and speaks HTTP.
package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// Timeout bounds one conversion, below the api's request timeout so a slow
// document is refused in words rather than cut off.
const Timeout = 20 * time.Second

// convertPath is where the service takes a document as a multipart part
// named files and answers with the PDF.
const convertPath = "/forms/libreoffice/convert"

// errorBodyLimit is how much of a refusal's body is kept for the log.
const errorBodyLimit = 512

var (
	// ErrUnavailable is a service that could not be reached or did not
	// answer in time; asking again later may work.
	ErrUnavailable = errors.New("the conversion service is not available")
	// ErrFailed is a document the service could not convert; asking again
	// would fail again.
	ErrFailed = errors.New("the document could not be converted")
	// ErrTooLarge is a conversion whose PDF is larger than the client takes.
	ErrTooLarge = errors.New("the converted document is too large")
)

// Converter turns one document into a PDF. The name's extension tells the
// service what the bytes are.
type Converter interface {
	ToPDF(ctx context.Context, name string, data []byte) ([]byte, error)
}

// Client calls the conversion service at its base URL. The URL is the
// operator's, like the bucket's, so it is not held to the outbound guard.
type Client struct {
	base      string
	http      *http.Client
	maxOutput int64
}

// New makes a client that takes PDFs of up to maxOutput bytes.
func New(baseURL string, maxOutput int64) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), http: &http.Client{}, maxOutput: maxOutput}
}

// ToPDF sends the document and reads the PDF back.
func (c *Client) ToPDF(ctx context.Context, name string, data []byte) ([]byte, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, name))
	header.Set("Content-Type", "application/octet-stream")
	part, err := form.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+convertPath, &body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		said, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return nil, fmt.Errorf("%w: %s answered %d: %s", classify(resp.StatusCode), convertPath, resp.StatusCode, strings.TrimSpace(string(said)))
	}
	pdf, err := io.ReadAll(io.LimitReader(resp.Body, c.maxOutput+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if int64(len(pdf)) > c.maxOutput {
		return nil, ErrTooLarge
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w: the answer is no PDF", ErrFailed)
	}
	return pdf, nil
}

// classify says whether a refusal is the document's fault, which asking
// again cannot mend, or the service's, which may pass.
func classify(status int) error {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return ErrFailed
	}
	return ErrUnavailable
}
