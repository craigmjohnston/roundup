package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

const (
	readoutGap          = 10
	customBadgeSize     = 17
	customBadgeIconSize = 13
	customBadgeOutset   = 4
	dismissInset        = 6
	dismissPadding      = 4
)

func (a *app) taskRow(r ui.ListRow, tk task.Task) {
	c := r.Context
	t := c.Theme()
	bg := ui.Transparent
	if r.Hovered() {
		bg = t.SurfaceHover
	}
	row := ui.Row(c).Padding(7, 10).Gap(10).Radius(8).Background(bg).AlignItems(ui.Center)
	row.Children(func() {
		a.lead(c, tk, bg)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			title, placeholder := rowTitle(tk)
			text := ui.Text(c, title)
			if placeholder {
				text = text.TextColor(t.TextMuted)
			}
			text.SingleLine()
			ui.Row(c).AlignItems(ui.Center).Children(func() { a.detailLine(c, tk) })
		})
		if r.Hovered() {
			ui.Row(c).Absolute().Top(4).Right(dismissInset).Padding(0, 0, 0, dismissPadding).Background(bg).AlignItems(ui.Center).Children(func() {
				ui.Button(c.Key("dismiss"), "").Label("Dismiss").Size(20, 20).Padding(0).Children(func() {
					ui.Icon(c, closeIcon).Size(10, 10)
				}).OnClick(func() { a.dismiss(tk) })
			})
		}
	})
	if row.RowClicked() {
		a.open(tk)
	}
}

func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
}

func statusCircle(c *ui.Context, st task.Status) {
	t := c.Theme()
	col := toneColor(t, st.Tone)
	bg := col.Alpha(0.16)
	if st.Tone == task.ToneNeutral || st.Tone == "" {
		bg = t.SurfaceHover
	}
	g := styleOf(st.Symbol).lead
	ui.Row(c).Size(leadSize, leadSize).Radius(leadSize / 2).Background(bg).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		sfsymbols.Symbol(c, st.Symbol).Size(g.size).Color(col).Weight(g.weight)
	})
}

// lead draws a row's 28-point round lead: its status circle, or the person's photo, or the source on a tile.
func (a *app) lead(c *ui.Context, tk task.Task, rowBackground ui.Color) {
	if tk.Lead == nil {
		a.leading(c, tk, rowBackground)
		return
	}
	ui.Box(c).Size(leadSize, leadSize).Children(func() {
		statusCircle(c, *tk.Lead)
		a.cornerBadge(c, tk, rowBackground)
	})
}

// cornerBadge draws the small mark on a lead's lower-right corner: the task's badge icon, or else its source's.
func (a *app) cornerBadge(c *ui.Context, tk task.Task, rowBackground ui.Color) {
	ring := c.Theme().Surface
	if rowBackground != ui.Transparent {
		ring = rowBackground
	}
	if tk.Badge != "" {
		ui.Row(c).Absolute().Bottom(-customBadgeOutset).Right(-customBadgeOutset).Size(customBadgeSize, customBadgeSize).Radius(customBadgeSize / 2).Background(ring).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			a.drawIcon(c, tk.Source, tk.Badge, customBadgeIconSize).Radius(3)
		})
		return
	}
	ui.Row(c).Absolute().Bottom(-badgeOutset).Right(-badgeOutset).Size(badgeSize, badgeSize).Radius(badgeSize / 2).Background(ring).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		a.sourceIcon(c, tk.Source, badgeIconSize)
	})
}

// detailLine draws the left of a row's second line, then its readouts, or its due date when it has none.
func (a *app) detailLine(c *ui.Context, tk task.Task) {
	if len(tk.Readouts) == 0 {
		muted(c, tk.Detail).Grow(1).Shrink(1).MinWidth(0)
		muted(c, dueLabel(tk.Due, time.Now()))
		return
	}
	if slices.ContainsFunc(tk.Readouts, func(r task.Readout) bool { return r.Alert }) {
		muted(c, tk.Detail).Shrink(0)
		ui.Row(c).Grow(1).MinWidth(readoutGap)
	} else {
		muted(c, tk.Detail).Grow(1).Shrink(1).MinWidth(0)
	}
	if slices.ContainsFunc(tk.Readouts, func(r task.Readout) bool { return r.Clock == task.ClockElapsed }) {
		c.After(time.Second)
	}
	ui.Row(c).Shrink(1).MinWidth(0).Gap(readoutGap).AlignItems(ui.Center).Children(func() {
		for _, ro := range tk.Readouts {
			a.readout(c, ro)
		}
	})
}

func (a *app) readout(c *ui.Context, ro task.Readout) {
	t := c.Theme()
	text := readoutText(ro, c.Now())
	if ro.Symbol == "" {
		muted(c, text).Shrink(0)
		return
	}
	col := toneColor(t, ro.Tone)
	st := styleOf(ro.Symbol)
	ui.Row(c).Shrink(1).MinWidth(0).Gap(st.readoutGap).AlignItems(ui.Center).Children(func() {
		sfsymbols.Symbol(c, ro.Symbol).Size(st.readout.size).Color(col).Weight(st.readout.weight)
		if ro.Alert {
			ui.Text(c, text).FontSize(11).TextColor(col).SingleLine().Shrink(1).MinWidth(0)
			return
		}
		muted(c, text)
	})
}
