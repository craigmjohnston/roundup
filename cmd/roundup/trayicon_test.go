package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

func TestReadoutCountsFailuresAndSuccesses(t *testing.T) {
	outcome := func(key string, o task.Outcome) task.Task {
		return task.Task{Source: "test", Key: key, Created: day(1), Outcome: o}
	}
	inFixed := func(tk task.Task) task.Task { tk.Section = sectionFixed; return tk }
	var s state
	s.AddSource(testInfo)
	s.Snapshot("test", []task.Task{
		item("a", "Alpha", 1),
		item("b", "Bravo", 2),
		outcome("m1", task.OutcomeFailed),
		inSection(outcome("p1", task.OutcomeFailed), sectionFirst),
		inSection(outcome("p2", ""), sectionFirst),
		inSection(outcome("p3", ""), sectionFirst),
		inFixed(outcome("d1", task.OutcomeFailed)),
		inFixed(outcome("d2", task.OutcomeSucceeded)),
		inFixed(outcome("d3", task.OutcomeSucceeded)),
		inFixed(outcome("d4", "")),
	})

	if want, got := (readout{total: "3", failed: 3, succeeded: 2}), s.Readout(); want != got {
		t.Errorf("want %+v, got %+v", want, got)
	}
}

func TestReadoutLeavesOutDismissedTasks(t *testing.T) {
	failed := task.Task{Source: "test", Key: "d1", Section: sectionFixed, Created: day(1), Outcome: task.OutcomeFailed}
	var s state
	s.Snapshot("test", []task.Task{failed})
	if err := s.Dismiss(failed); err != nil {
		t.Fatal(err)
	}

	if want, got := (readout{total: "0"}), s.Readout(); want != got {
		t.Errorf("want %+v, got %+v", want, got)
	}
}

func TestReadoutIcon(t *testing.T) {
	decode := func(t *testing.T, r readout, dark bool, scale float32) image.Image {
		t.Helper()
		data, _, err := readoutIcons(r, dark, readoutDefaultHeight, scale)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if dir := os.Getenv("ROUNDUP_SNAPSHOT_DIR"); dir != "" {
			mode := "light"
			if dark {
				mode = "dark"
			}
			name := fmt.Sprintf("tray-%s-%d-%d-%s-%gx.png", r.total, r.failed, r.succeeded, mode, scale)
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return img
	}

	icon := decode(t, readout{}, false, 2)
	counted := decode(t, readout{total: "5"}, false, 2)
	full := decode(t, readout{total: "5", failed: 3, succeeded: 1}, false, 2)
	fullDark := decode(t, readout{total: "5", failed: 3, succeeded: 1}, true, 2)
	fullAtOne := decode(t, readout{total: "5", failed: 3, succeeded: 1}, true, 1)

	for name, img := range map[string]image.Image{"icon": icon, "counted": counted, "full": full} {
		if h := img.Bounds().Dy(); h != readoutDefaultHeight*2 {
			t.Errorf("%s: want height %d, got %d", name, readoutDefaultHeight*2, h)
		}
	}
	if h := fullAtOne.Bounds().Dy(); h != readoutDefaultHeight {
		t.Errorf("at one pixel per point: want height %d, got %d", readoutDefaultHeight, h)
	}
	if !(icon.Bounds().Dx() < counted.Bounds().Dx() && counted.Bounds().Dx() < full.Bounds().Dx()) {
		t.Errorf("want widths to grow with content, got %d, %d, %d", icon.Bounds().Dx(), counted.Bounds().Dx(), full.Bounds().Dx())
	}
	if full.Bounds().Dx() >= readoutMaxWidth*2 {
		t.Errorf("want the readout cropped to its content, got the full width %d", full.Bounds().Dx())
	}
	if _, _, _, a := full.At(0, 0).RGBA(); a != 0 {
		t.Errorf("want a transparent corner outside the capsule, got alpha %d", a)
	}
	mid := full.Bounds().Min.Y + full.Bounds().Dy()/2
	if _, _, _, a := full.At(full.Bounds().Min.X+1, mid).RGBA(); a == 0 {
		t.Error("want the capsule to reach the image's left edge, got a transparent pixel there")
	}
	if _, _, _, a := full.At(full.Bounds().Max.X-2, mid).RGBA(); a == 0 {
		t.Error("want the capsule to reach the image's right edge, got a transparent pixel there")
	}
	if sameImage(full, fullDark) {
		t.Error("want the dark menu bar readout drawn in different colours")
	}
}

func TestReadoutHeightFor(t *testing.T) {
	tests := []struct {
		name       string
		itemHeight int
		want       int
	}{
		{name: "unknown item height uses the default", itemHeight: 0, want: 24},
		{name: "a 30 point menu bar gives a 24 point capsule", itemHeight: 30, want: 24},
		{name: "a short menu bar is not squeezed below the minimum", itemHeight: 24, want: 22},
		{name: "a tall menu bar is capped", itemHeight: 37, want: 26},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := readoutHeightFor(tc.itemHeight); got != tc.want {
				t.Errorf("want %d, got %d", tc.want, got)
			}
		})
	}
}

func TestWithScaleRecordsPixelsPerPoint(t *testing.T) {
	var plain bytes.Buffer
	if err := png.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}

	scaled := withScale(plain.Bytes(), 2)

	if _, err := png.Decode(bytes.NewReader(scaled)); err != nil {
		t.Fatalf("want a PNG that still decodes, got %v", err)
	}
	at := bytes.Index(scaled, []byte("pHYs"))
	if at != 8+25+4 {
		t.Fatalf("want a pHYs chunk straight after the header, got index %d", at)
	}
	perMetre := binary.BigEndian.Uint32(scaled[at+4:])
	if want := uint32(5669); perMetre != want {
		t.Errorf("want %d pixels per metre (144 per inch), got %d", want, perMetre)
	}
	if unit := scaled[at+12]; unit != 1 {
		t.Errorf("want the metre unit, got %d", unit)
	}
}

func sameImage(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}

func TestReadoutGlyphsAreCentredOnTheirDiscs(t *testing.T) {
	for height := readoutMinHeight; height <= readoutMaxHeight; height++ {
		for _, scale := range []float32{1, 2} {
			for _, dark := range []bool{true, false} {
				data, _, err := readoutIcons(readout{total: "5", failed: 3, succeeded: 1}, dark, height, scale)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(decoded.Bounds())
				draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
				red, green := ui.Hex("#c81e12"), ui.Hex("#0f7a36")
				if dark {
					red, green = ui.Hex("#ff8a80"), ui.Hex("#5ee68c")
				}
				for name, disc := range map[string]ui.Color{"cross": red, "tick": green} {
					box, ok := colourBounds(img, disc)
					if !ok {
						t.Fatalf("height %d scale %g dark %v: no %s disc", height, scale, dark, name)
					}
					if box.Dx() != box.Dy() {
						t.Errorf("height %d scale %g dark %v: %s disc is %dx%d, not round", height, scale, dark, name, box.Dx(), box.Dy())
					}
					cx, cy := float64(box.Min.X+box.Max.X)/2, float64(box.Min.Y+box.Max.Y)/2
					radius := float64(box.Dx())/2 - 1.5
					var sum, sx, sy float64
					for y := box.Min.Y; y < box.Max.Y; y++ {
						for x := box.Min.X; x < box.Max.X; x++ {
							dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
							if dx*dx+dy*dy > radius*radius {
								continue
							}
							p := img.RGBAAt(x, y)
							w := math.Abs(float64(p.R)-float64(disc.R)) + math.Abs(float64(p.G)-float64(disc.G)) + math.Abs(float64(p.B)-float64(disc.B))
							sum += w
							sx += w * dx
							sy += w * dy
						}
					}
					if sum == 0 {
						t.Fatalf("height %d scale %g dark %v: the %s disc has no glyph", height, scale, dark, name)
					}
					// The tick's ink is lopsided by design, so half a pixel is the tightest bound that holds for both glyphs.
					if offX, offY := sx/sum, sy/sum; math.Abs(offX) > 0.5 || math.Abs(offY) > 0.5 {
						t.Errorf("height %d scale %g dark %v: %s ink is %+.2f, %+.2f px from the disc's centre", height, scale, dark, name, offX, offY)
					}
				}
			}
		}
	}
}

func TestHighlightedReadoutIsTheIconWithoutItsCapsule(t *testing.T) {
	for _, dark := range []bool{true, false} {
		for _, scale := range []float32{1, 2} {
			icon, highlighted, err := readoutIcons(readout{total: "5", failed: 3, succeeded: 1}, dark, readoutDefaultHeight, scale)
			if err != nil {
				t.Fatal(err)
			}
			withPill, err := png.Decode(bytes.NewReader(icon))
			if err != nil {
				t.Fatal(err)
			}
			bare, err := png.Decode(bytes.NewReader(highlighted))
			if err != nil {
				t.Fatal(err)
			}
			if withPill.Bounds() != bare.Bounds() {
				t.Fatalf("want the same size, got %v and %v", withPill.Bounds(), bare.Bounds())
			}
			edge := image.Pt(withPill.Bounds().Min.X+int(2*scale), withPill.Bounds().Dy()/2)
			if _, _, _, a := withPill.At(edge.X, edge.Y).RGBA(); a == 0 {
				t.Errorf("dark=%v scale=%v: want the capsule at %v in the icon", dark, scale, edge)
			}
			if _, _, _, a := bare.At(edge.X, edge.Y).RGBA(); a != 0 {
				t.Errorf("dark=%v scale=%v: want nothing at %v in the highlighted icon", dark, scale, edge)
			}
		}
	}
}
