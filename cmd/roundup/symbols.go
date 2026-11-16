package main

import (
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

type glyph struct {
	size   float32
	weight sfsymbols.Weight
}

// symbolStyle is how one SF Symbol is drawn: as a row's lead, and beside a readout.
type symbolStyle struct {
	lead       glyph
	readout    glyph
	readoutGap float32
}

// symbolStyles tunes symbols whose natural size or weight looks off in a lead circle or beside a readout; the rest use defaultSymbol.
var symbolStyles = map[string]symbolStyle{
	"plusminus":        {lead: glyph{15, sfsymbols.Semibold}},
	"bubble.left.fill": {lead: glyph{15, sfsymbols.Regular}, readout: glyph{11, sfsymbols.Regular}, readoutGap: 4},
	"xmark":            {lead: glyph{13, sfsymbols.Bold}},
	"checkmark":        {lead: glyph{14, sfsymbols.Bold}},
}

var defaultSymbol = symbolStyle{lead: glyph{15, sfsymbols.Bold}, readout: glyph{10, sfsymbols.Bold}, readoutGap: 5}

func styleOf(symbol string) symbolStyle {
	st := symbolStyles[symbol]
	if st.lead.size == 0 {
		st.lead = defaultSymbol.lead
	}
	if st.readout.size == 0 {
		st.readout = defaultSymbol.readout
	}
	if st.readoutGap == 0 {
		st.readoutGap = defaultSymbol.readoutGap
	}
	return st
}

// toneColor is the colour of a tone; neutral is the muted text colour.
func toneColor(t *ui.Theme, tone task.Tone) ui.Color {
	switch tone {
	case task.ToneAccent:
		return t.Accent
	case task.ToneSuccess:
		return t.Success
	case task.ToneWarning:
		return t.Warning
	case task.ToneDanger:
		return t.Danger
	}
	return t.TextMuted
}
