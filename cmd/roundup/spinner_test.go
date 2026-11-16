package main

import (
	"bytes"
	"image/png"
	"testing"
)

func TestSpinnerFrames(t *testing.T) {
	frames := spinnerFrames()

	if len(frames) != spinnerSpokes {
		t.Fatalf("want %d frames, got %d", spinnerSpokes, len(frames))
	}
	for i, f := range frames {
		img, err := png.Decode(bytes.NewReader(f))
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
			t.Errorf("frame %d: want 32x32, got %dx%d", i, b.Dx(), b.Dy())
		}
		if i > 0 && bytes.Equal(frames[i-1], f) {
			t.Errorf("frame %d is the same as frame %d", i, i-1)
		}
	}
}

func TestSpokeAlpha(t *testing.T) {
	tests := []struct {
		name   string
		dx, dy float64
		frame  int
		want   uint8
	}{
		{name: "the leading spoke is solid", dx: 0, dy: -10, frame: 0, want: 255},
		{name: "the spoke one step behind is fainter", dx: 0, dy: -10, frame: 1, want: 227},
		{name: "the spoke just ahead is the faintest", dx: 0, dy: -10, frame: 7, want: 59},
		{name: "the centre is empty", dx: 0, dy: 0, frame: 0, want: 0},
		{name: "between spokes is empty", dx: 4, dy: -10, frame: 0, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := spokeAlpha(tc.dx, tc.dy, tc.frame); got != tc.want {
				t.Errorf("want %d, got %d", tc.want, got)
			}
		})
	}
}
