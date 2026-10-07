package example

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
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

// The board the showcase's gallery shows beside the picture: columns of cards.
const (
	boardColumns = 3
	cardHeight   = 36
	cardGap      = 12
)

// boardCards is how many cards each of the board's columns holds.
var boardCards = []int{4, 2, 3}

// Board is the second PNG of the showcase's gallery, as large as Picture.
func Board() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, pictureWidth, pictureHeight))
	fill(img, img.Bounds(), paper)
	width := (pictureWidth - (boardColumns+1)*margin/2) / boardColumns
	for col, cards := range boardCards {
		left := margin/2 + col*(width+margin/2)
		column := image.Rect(left, margin, left+width, pictureHeight-margin)
		fill(img, column, sheet)
		fill(img, image.Rect(column.Min.X+cardGap, column.Min.Y+cardGap, column.Max.X-cardGap, column.Min.Y+cardGap+lineHeight), accent)
		top := column.Min.Y + 2*cardGap + lineHeight
		for range cards {
			fill(img, image.Rect(column.Min.X+cardGap, top, column.Max.X-cardGap, top+cardHeight), ink)
			top += cardHeight + cardGap
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// The showcase's cover: two ranges of hills under a sky that lightens toward
// them, wide and low as a cover is shown, so any cut around its focus reads.
const (
	coverWidth  = 1600
	coverHeight = 400
	// CoverFocusX and CoverFocusY are where the cover stays in view, in
	// percent from the left and the top: the nearer hills' highest point.
	CoverFocusX = 30
	CoverFocusY = 60
)

// coverHill is one range of hills: its base line and wave, as shares of the
// cover's height, and how many waves span the cover.
type coverHill struct {
	base, swell, waves float64
	colour             color.RGBA
}

var coverHills = []coverHill{
	{base: 0.55, swell: 0.12, waves: 1.6, colour: color.RGBA{R: 0x9f, G: 0xbf, B: 0xf5, A: 0xff}},
	{base: 0.72, swell: 0.10, waves: 1.0, colour: accent},
}

// Cover is the PNG the showcase shows as its cover picture.
func Cover() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, coverWidth, coverHeight))
	for y := range coverHeight {
		share := float64(y) / coverHeight
		sky := color.RGBA{R: mix(0xdc, paper.R, share), G: mix(0xe8, paper.G, share), B: mix(0xfc, paper.B, share), A: 0xff}
		fill(img, image.Rect(0, y, coverWidth, y+1), sky)
	}
	for _, h := range coverHills {
		for x := range coverWidth {
			wave := math.Sin(float64(x) / coverWidth * h.waves * 2 * math.Pi)
			top := int((h.base - h.swell*wave) * coverHeight)
			fill(img, image.Rect(x, top, x+1, coverHeight), h.colour)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// mix is the share of the way from a to b.
func mix(a, b uint8, share float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*share)
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}
