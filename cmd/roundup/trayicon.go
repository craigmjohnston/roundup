package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/draw"
	"image/png"
	"math"
	"strconv"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

const (
	readoutDefaultHeight = 24
	readoutMinHeight     = 22
	readoutMaxHeight     = 26
	readoutMaxWidth      = 280
	readoutMargin        = 3
)

// readoutHeightFor is the capsule's height for a menu bar item that is itemHeight points tall, or the default when that is unknown.
func readoutHeightFor(itemHeight int) int {
	if itemHeight <= 0 {
		return readoutDefaultHeight
	}
	return min(max(itemHeight-2*readoutMargin, readoutMinHeight), readoutMaxHeight)
}

// readoutIcons draws the menu bar readout for a light or dark menu bar, height points tall at scale pixels per point,
// as PNGs cropped to the capsule that record their scale, so macOS shows them at height points.
// icon has the capsule; highlighted is the same size without it, for while macOS draws its own highlight behind the item.
// The capsule is dark on a dark bar and light on a light one, so the colours stay readable over a tinted wallpaper.
func readoutIcons(r readout, dark bool, height int, scale float32) (icon, highlighted []byte, err error) {
	fg, red, green, pill := ui.Hex("#1c1e22"), ui.Hex("#c81e12"), ui.Hex("#0f7a36"), ui.RGBA(255, 255, 255, 0.33)
	onRed, onGreen := ui.Hex("#ffffff"), ui.Hex("#ffffff")
	if dark {
		fg, red, green, pill = ui.Hex("#ffffff"), ui.Hex("#ff8a80"), ui.Hex("#5ee68c"), ui.RGBA(0, 0, 0, 0.17)
		onRed, onGreen = ui.Hex("#6b130c"), ui.Hex("#0b4a22")
	}
	withPill := renderReadout(r, height, scale, fg, red, green, onRed, onGreen, pill)
	bare := renderReadout(r, height, scale, fg, red, green, onRed, onGreen, ui.Transparent)
	crop := cropToInk(withPill).Bounds()
	if icon, err = encodeReadout(withPill.SubImage(crop), scale); err != nil {
		return nil, nil, err
	}
	if highlighted, err = encodeReadout(bare.SubImage(crop), scale); err != nil {
		return nil, nil, err
	}
	return icon, highlighted, nil
}

func renderReadout(r readout, height int, scale float32, fg, red, green, onRed, onGreen, pill ui.Color) *image.RGBA {
	h := float32(height)
	view := func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Row(c).Height(h).Children(func() {
			ui.Row(c).Height(h).Shrink(0).Padding(0, h/2).Gap(h / 2).Radius(h / 2).Background(pill).AlignItems(ui.Center).Children(func() {
				readoutPart(c, h, fg, r.total, func() {
					sfsymbols.Symbol(c, "checklist").Size(h * 0.7).Color(fg).Weight(sfsymbols.Bold)
				})
				if r.failed > 0 {
					readoutPart(c, h, fg, strconv.Itoa(r.failed), func() { statusDisc(c, h, red) })
				}
				if r.succeeded > 0 {
					readoutPart(c, h, fg, strconv.Itoa(r.succeeded), func() { statusDisc(c, h, green) })
				}
			})
		})
	}
	rendered := ui.Render(view, readoutMaxWidth, height, scale)
	if r.failed > 0 {
		stampGlyph(rendered, red, "xmark", onRed)
	}
	if r.succeeded > 0 {
		stampGlyph(rendered, green, "checkmark", onGreen)
	}
	return rendered
}

func encodeReadout(img image.Image, scale float32) ([]byte, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return withScale(b.Bytes(), scale), nil
}

func readoutPart(c *ui.Context, height float32, textCol ui.Color, count string, icon func()) {
	ui.Row(c).Gap(height / 4).Shrink(0).AlignItems(ui.Center).Children(func() {
		icon()
		if count != "" {
			// Digits sit low in their line box, so the text is raised to centre them on the symbols.
			ui.Text(c, count).FontSize(height * 0.57).FontWeight(500).TextColor(textCol).SingleLine().Top(-height / 30)
		}
	})
}

// statusDisc is a plain colour disc; stampGlyph draws its glyph once the disc's pixels are known.
func statusDisc(c *ui.Context, height float32, col ui.Color) {
	d := float32(int(height*0.625 + 0.5))
	ui.Row(c).Size(d, d).Radius(d / 2).Background(col).Shrink(0)
}

// glyphShare is the glyph's point size as a share of its disc's width.
const glyphShare = 0.62

// stampGlyph draws a symbol on the disc of colour disc in img, centring the symbol's ink on the disc's pixels.
// Layout centres a symbol's box, not its ink, which leaves the ink up to a pixel off at some sizes.
func stampGlyph(img *image.RGBA, disc ui.Color, name string, col ui.Color) {
	box, ok := colourBounds(img, disc)
	if !ok {
		return
	}
	centreX, centreY := float64(box.Min.X+box.Max.X)/2, float64(box.Min.Y+box.Max.Y)/2
	var best *image.RGBA
	var bestAt image.Point
	bestErr := math.Inf(1)
	base := int(float64(box.Dx())*glyphShare + 0.5)
	for _, size := range []int{base + 1, base + 2, base} {
		glyph := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			sfsymbols.Symbol(c, name).Size(float32(size)).Color(col).Weight(sfsymbols.Bold)
		}, size+4, size+4, 1)
		gx, gy, ok := inkCentre(glyph)
		if !ok {
			continue
		}
		at := image.Pt(int(math.Round(centreX-gx)), int(math.Round(centreY-gy)))
		if err := math.Abs(centreX-gx-float64(at.X)) + math.Abs(centreY-gy-float64(at.Y)); err < bestErr-0.05 {
			best, bestAt, bestErr = glyph, at, err
		}
	}
	if best != nil {
		draw.Draw(img, best.Bounds().Add(bestAt), best, image.Point{}, draw.Over)
	}
}

// colourBounds is the box around the opaque pixels of exactly this colour.
func colourBounds(img *image.RGBA, col ui.Color) (image.Rectangle, bool) {
	var box image.Rectangle
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if p := img.RGBAAt(x, y); p.A == 255 && p.R == col.R && p.G == col.G && p.B == col.B {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box, !box.Empty()
}

// inkCentre is the alpha-weighted centre of an image's pixels, in pixel-edge coordinates.
func inkCentre(img *image.RGBA) (x, y float64, ok bool) {
	var sum, sx, sy float64
	b := img.Bounds()
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			a := float64(img.RGBAAt(px, py).A)
			sum += a
			sx += a * (float64(px) + 0.5)
			sy += a * (float64(py) + 0.5)
		}
	}
	if sum == 0 {
		return 0, 0, false
	}
	return sx / sum, sy / sum, true
}

// cropToInk trims the transparent columns to the right of the drawing, keeping the full height.
func cropToInk(img *image.RGBA) image.Image {
	b := img.Bounds()
	right := b.Min.X
	for x := b.Max.X - 1; x >= b.Min.X && right == b.Min.X; x-- {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if img.RGBAAt(x, y).A != 0 {
				right = x + 1
				break
			}
		}
	}
	if right == b.Min.X {
		right = b.Min.X + 1
	}
	return img.SubImage(image.Rect(b.Min.X, b.Min.Y, right, b.Max.Y))
}

// withScale adds a pHYs chunk to a PNG so that readers size it at 72 points per inch times scale.
func withScale(data []byte, scale float32) []byte {
	const signatureAndIHDR = 8 + 25
	if len(data) < signatureAndIHDR {
		return data
	}
	pixelsPerMetre := uint32(float32(72) * scale / 0.0254)
	body := make([]byte, 9)
	binary.BigEndian.PutUint32(body[0:], pixelsPerMetre)
	binary.BigEndian.PutUint32(body[4:], pixelsPerMetre)
	body[8] = 1
	chunk := make([]byte, 0, 21)
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(len(body)))
	chunk = append(chunk, "pHYs"...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))

	out := make([]byte, 0, len(data)+len(chunk))
	out = append(out, data[:signatureAndIHDR]...)
	out = append(out, chunk...)
	return append(out, data[signatureAndIHDR:]...)
}
