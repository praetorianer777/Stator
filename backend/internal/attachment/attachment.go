// Package attachment keeps the files on pages, their bytes in the object
// store and what is known about them in the database, as Armature does.
package attachment

import (
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
