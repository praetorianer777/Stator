// Package attachment keeps the files on pages, their bytes in the object
// store and what is known about them in the database, as Armature does.
package attachment

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DefaultMaxSize is the largest file a page takes unless STATOR_UPLOAD_LIMIT
// says otherwise, Armature's default.
const DefaultMaxSize int64 = 50 << 20

// Attachment is what is known about a file on a page.
type Attachment struct {
	ID     uuid.UUID `json:"id"`
	PageID uuid.UUID `json:"pageId"`
	// FileName is the name it was uploaded with, which a download keeps.
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	// Width and Height are an image's, in pixels; null for anything else.
	Width          *int      `json:"width"`
	Height         *int      `json:"height"`
	UploadedByName string    `json:"uploadedByName"`
	CreatedAt      time.Time `json:"createdAt"`
}

var (
	// ErrNotFound is also the answer for a file on a page the caller may not see.
	ErrNotFound = errors.New("attachment not found")
	// ErrTooLarge is what every TooLargeError is.
	ErrTooLarge = errors.New("that file is too large")
	// ErrEmpty is returned for an upload with no bytes in it.
	ErrEmpty = errors.New("that file is empty; choose a file with something in it")
)

// TooLargeError refuses a file over the limit, naming the limit.
type TooLargeError struct{ Limit int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("that file is too large: files on a page are up to %s; make it smaller or split it", Size(e.Limit))
}

func (e *TooLargeError) Is(target error) bool { return target == ErrTooLarge }

// Size writes a byte count the way a person reads a limit: 50 MB, 512 KB.
func Size(n int64) string {
	switch {
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
