package attachment

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"

	_ "golang.org/x/image/webp"
)

// measured are the types whose size in pixels is read, the images a page
// shows in place.
var measured = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// dimensions reads an image's size from its header alone; anything that is
// not one, or does not decode, has none.
func dimensions(contentType string, data []byte) (width, height *int) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !measured[mediaType] {
		return nil, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, nil
	}
	return &cfg.Width, &cfg.Height
}
