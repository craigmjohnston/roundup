package main

import "math"

// Heights of the panel's parts in points; TestLayoutMatchesTheRenderedPanel checks them against a rendered panel.
const (
	flyoutWidth      = 380
	maxPanelHeight   = 640
	headerHeight     = 42.31
	rowHeight        = 44.3
	headingHeight    = 31
	listPadding      = 12
	sectionsPadding  = 6
	dividerHeight    = 1
	noticeHeight     = 44
	emptyStripHeight = 71.3
	emptyBodyHeight  = 120
	loadingHeight    = 60
)

// panelLayout is the height each part of the panel gets, and the panel's total height.
type panelLayout struct {
	main     float32
	sections float32
	fixed    float32
	total    float32
}

// layout sizes the panel to its content up to maxPanelHeight: fixed sections always show in full, and the task list and the other sections share what is left.
func (s *state) layout() panelLayout {
	shared, fixedRows := s.scrollingRows(), s.fixedRows()

	var l panelLayout
	reserved := float32(headerHeight) + float32(len(s.SignedOutIDs()))*(noticeHeight+dividerHeight)
	if len(fixedRows) > 0 {
		l.fixed = rowsHeight(fixedRows) + sectionsPadding
		reserved += l.fixed + dividerHeight
	}
	var sharedNeed float32
	if len(shared) > 0 {
		sharedNeed = rowsHeight(shared) + sectionsPadding
		reserved += dividerHeight
	}

	switch s.Mode() {
	case modeList:
		mainNeed := listPadding + float32(len(s.tasks))*rowHeight
		avail := max(maxPanelHeight-reserved, 2*rowHeight)
		l.main, l.sections = shareHeights(avail, mainNeed, sharedNeed)
	case modeNoSources:
		l.main = emptyBodyHeight
	case modeEmpty:
		l.main, l.sections = emptyStripHeight, sharedNeed
		if len(shared)+len(fixedRows) == 0 {
			l.main = emptyBodyHeight
		}
		l.sections = min(l.sections, max(maxPanelHeight-reserved-l.main, 2*rowHeight))
	default:
		l.main = loadingHeight
	}
	l.total = float32(math.Ceil(float64(reserved + l.main + l.sections)))
	return l
}

// shareHeights gives two lists what they need when both fit; otherwise the smaller need is met up to half and the other takes the rest.
func shareHeights(avail, a, b float32) (float32, float32) {
	half := avail / 2
	switch {
	case a+b <= avail:
		return a, b
	case a <= half:
		return a, avail - a
	case b <= half:
		return avail - b, b
	}
	return half, half
}

func rowsHeight(rows []row) float32 {
	var h float32
	for _, r := range rows {
		if r.heading != "" {
			h += headingHeight
		} else {
			h += rowHeight
		}
	}
	return h
}
