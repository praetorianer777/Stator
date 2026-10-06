package example

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// The showcase's picture: a page of lines beside a bar chart, drawn here so
// the example ships no binary file and holds nobody's artwork.
const (
	pictureWidth  = 640
	pictureHeight = 320
	margin        = 32
	lineHeight    = 14
	lineGap       = 12
	barWidth      = 40
	barGap        = 20
)

var (
	paper  = color.RGBA{R: 0xf5, G: 0xf7, B: 0xfa, A: 0xff}
	ink    = color.RGBA{R: 0x9a, G: 0xa5, B: 0xb4, A: 0xff}
	accent = color.RGBA{R: 0x2f, G: 0x6f, B: 0xeb, A: 0xff}
	sheet  = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// barShares are the bars' heights as shares of the room they have, in percent.
var barShares = []int{35, 55, 45, 80, 65}

// Picture is the PNG the showcase shows as its image block.
func Picture() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, pictureWidth, pictureHeight))
	fill(img, img.Bounds(), paper)
	half := pictureWidth / 2
	page := image.Rect(margin, margin, half-margin/2, pictureHeight-margin)
	fill(img, page, sheet)
	for y, i := page.Min.Y+margin, 0; y+lineHeight < page.Max.Y-margin/2; y, i = y+lineHeight+lineGap, i+1 {
		right := page.Max.X - margin
		if i%3 == 2 {
			right -= (page.Dx() - 2*margin) / 3
		}
		c := ink
		if i == 0 {
			c = accent
		}
		fill(img, image.Rect(page.Min.X+margin, y, right, y+lineHeight), c)
	}
	chart := image.Rect(half+margin/2, margin, pictureWidth-margin, pictureHeight-margin)
	fill(img, chart, sheet)
	room := chart.Dy() - 2*margin
	for i, share := range barShares {
		x := chart.Min.X + margin + i*(barWidth+barGap)
		top := chart.Max.Y - margin - room*share/100
		fill(img, image.Rect(x, top, x+barWidth, chart.Max.Y-margin), accent)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}
