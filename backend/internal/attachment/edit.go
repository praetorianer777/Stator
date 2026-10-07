package attachment

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// editable are the pictures a browser's canvas reads and writes again in
// their own type. A GIF may move, and redrawing it would keep one frame.
var editable = map[string]string{"image/png": "PNG", "image/jpeg": "JPEG", "image/webp": "WebP"}

// Editable says whether a file of this type can be cropped and drawn on.
func Editable(contentType string) bool {
	_, ok := editable[mediaType(contentType)]
	return ok
}

func mediaType(contentType string) string {
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return parsed
}

// ErrNotEditable refuses to edit a file that is not a picture the browser redraws.
var ErrNotEditable = errors.New("only PNG, JPEG and WebP pictures can be edited here; download the file to edit it elsewhere, then upload it under the same name")

// EditTypeError refuses an edited picture that is not of its file's type,
// since the file's name and every reader expect that type.
type EditTypeError struct {
	Name string
	// Want is the type the file is; Got what was sent, empty when it is no picture.
	Want, Got string
}

func (e *EditTypeError) Error() string {
	want := editable[e.Want]
	if e.Got == "" {
		return fmt.Sprintf("the edited picture could not be read as a picture; send %s as a %s picture", e.Name, want)
	}
	return fmt.Sprintf("the file %s is a %s picture and the edited one is a %s; send it as a %s picture, or upload it under a new name", e.Name, want, editable[e.Got], want)
}

// Edit saves a picture cropped or drawn on in the browser as the next version
// of the file it was drawn on. The version drawn on is kept, as every one is.
func (s *Service) Edit(ctx context.Context, actor perm.Actor, id uuid.UUID, in UploadInput) (*Attachment, db.LSN, error) {
	data, err := s.readWhole(in.Body)
	if err != nil {
		return nil, 0, err
	}
	// The bytes say what they are; a declared type could name anything.
	sent := mediaType(http.DetectContentType(data))
	width, height := dimensions(sent, data)
	if _, ok := editable[sent]; !ok || width == nil {
		sent = ""
	}

	var edited *Attachment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, p, err := find(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !p.Can.Edit {
			return p.Refusal(perm.EditPages)
		}
		want := mediaType(found.ContentType)
		if !Editable(want) {
			return ErrNotEditable
		}
		if sent != want {
			return &EditTypeError{Name: found.FileName, Want: want, Got: sent}
		}
		from := found.Version
		edited, err = s.add(ctx, tx, actor, found.PageID, version{
			name: found.FileName, contentType: sent, data: data,
			width: width, height: height, editedFrom: &from,
		})
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return edited, lsn, nil
}
