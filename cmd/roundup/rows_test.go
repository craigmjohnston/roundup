package main

import (
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

const (
	sectionFirst = "First"
	sectionFixed = "Fixed"
)

var testInfo = markInfo("test", task.Section{Name: sectionFirst}, task.Section{Name: sectionFixed, Order: 2, Fixed: true})

var (
	markRed  = color.NRGBA{R: 0xcc, A: 255}
	markBlue = color.NRGBA{B: 0xcc, A: 255}
)

// markInfo describes a source with a red mark as its icon and a blue one as a badge.
func markInfo(id string, sections ...task.Section) task.Info {
	square := func(fill string) task.Icon {
		return task.Icon{SVG: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="11" height="11"><rect width="11" height="11" fill="` + fill + `"/></svg>`)}
	}
	return task.Info{ID: id, Name: "Test", Icon: "mark", Icons: map[string]task.Icon{"mark": square("#cc0000"), "alt": square("#0000cc")}, Sections: sections}
}

func successLead(symbol string) task.Status {
	return task.Status{Symbol: symbol, Tone: task.ToneSuccess}
}
func neutralLead(symbol string) task.Status {
	return task.Status{Symbol: symbol, Tone: task.ToneNeutral}
}

func leadRow(key, title string, lead task.Status, detail string, readouts ...task.Readout) task.Task {
	return task.Task{Source: "test", Key: key, Title: title, URL: "https://example.com/" + key, Created: day(5), Section: sectionFirst, Lead: &lead, Detail: detail, Readouts: readouts}
}

func fixedLeadRow(key, title string, lead task.Status, detail string, readouts ...task.Readout) task.Task {
	tk := leadRow(key, title, lead, detail, readouts...)
	tk.Section = sectionFixed
	return tk
}

func elapsed(since time.Time) task.Readout {
	return task.Readout{Since: since, Clock: task.ClockElapsed}
}
func ago(since time.Time) task.Readout    { return task.Readout{Since: since, Clock: task.ClockAgo} }
func fixedClock(text string) task.Readout { return task.Readout{Text: text} }
func counted(symbol string, tone task.Tone, text string) task.Readout {
	return task.Readout{Symbol: symbol, Tone: tone, Text: text}
}
func failure(text string) task.Readout {
	return task.Readout{Symbol: "xmark", Tone: task.ToneDanger, Text: text, Alert: true}
}

func (p *panel) box(text string) ui.Rect {
	p.t.Helper()
	box, ok := p.tt.Find(text)
	if !ok {
		p.t.Fatalf("%q not found; texts: %q", text, p.tt.Texts())
	}
	return box
}

func (p *panel) symbolsBesideTitle(title string) []string {
	p.t.Helper()
	box := p.box(title)
	var names []string
	for _, a := range p.tt.AccessAll() {
		if a.Bounds.X < box.X && a.Bounds.Y >= box.Y-10 && a.Bounds.Y < box.Y+10 && a.Bounds.W > 0 && a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return names
}

func TestEachLeadShowsItsSymbol(t *testing.T) {
	tests := []struct {
		name string
		lead task.Status
	}{
		{"neutral", task.Status{Symbol: "ellipsis", Tone: task.ToneNeutral}},
		{"accent", task.Status{Symbol: "plusminus", Tone: task.ToneAccent}},
		{"accent speech bubble", task.Status{Symbol: "bubble.left.fill", Tone: task.ToneAccent}},
		{"danger", task.Status{Symbol: "xmark", Tone: task.ToneDanger}},
		{"warning", task.Status{Symbol: "ellipsis", Tone: task.ToneWarning}},
		{"success", task.Status{Symbol: "checkmark", Tone: task.ToneSuccess}},
		{"a symbol with no tuning", task.Status{Symbol: "star.fill", Tone: task.ToneAccent}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newPanel(t, leadRow("k", "Title "+tc.name, tc.lead, "w#1"))
			p.tt.Frame()
			if got := p.symbolsBesideTitle("Title " + tc.name); !slices.Contains(got, tc.lead.Symbol) {
				t.Errorf("symbols beside the title: want %q among %q", tc.lead.Symbol, got)
			}
		})
	}
}

func TestSymbolStylesFallBackToTheDefault(t *testing.T) {
	if got, want := styleOf("star.fill"), defaultSymbol; got != want {
		t.Errorf("unlisted symbol: want %+v, got %+v", want, got)
	}
	if got := styleOf("plusminus"); got.lead != (glyph{15, sfsymbols.Semibold}) || got.readout != defaultSymbol.readout {
		t.Errorf("plusminus: want its own lead and the default readout, got %+v", got)
	}
	if got := styleOf("bubble.left.fill"); got.readoutGap != 4 || got.readout.size != 11 {
		t.Errorf("bubble.left.fill: want its own readout, got %+v", got)
	}
}

func TestReadoutsShowBesideTheDetailInOrder(t *testing.T) {
	p := newPanel(t,
		leadRow("a", "Alpha", neutralLead("ellipsis"), "widgets#14667", counted("bubble.left.fill", task.ToneNeutral, "8"), counted("xmark", task.ToneDanger, "11/12")),
		leadRow("b", "Bravo", neutralLead("ellipsis"), "cli#4", counted("bubble.left.fill", task.ToneNeutral, "9"), counted("circle.fill", task.ToneWarning, "2/5")),
		leadRow("c", "Charlie", successLead("checkmark"), "x#1"))
	p.tt.Frame()

	for _, text := range []string{"widgets#14667", "cli#4", "x#1", "11/12", "2/5", "8", "9"} {
		p.assertShows(text, true)
	}
	if x1, x2 := p.box("widgets#14667").X, p.box("cli#4").X; x1 != x2 {
		t.Errorf("detail x: %v and %v differ", x1, x2)
	}
	right := func(text string) float32 { b := p.box(text); return b.X + b.W }
	if r1, r2 := right("11/12"), right("2/5"); r1 != r2 {
		t.Errorf("last readout right edge: %v and %v differ", r1, r2)
	}
	if right("11/12") < panelWidth-40 {
		t.Errorf("last readout ends at %v, want it at the row's right edge", right("11/12"))
	}
	if gap := p.box("11/12").X - right("8"); gap < readoutGap || gap > readoutGap+20 {
		t.Errorf("first readout ends %v before the next readout's text, want them side by side", gap)
	}
	for _, name := range []string{"Bravo", "Charlie"} {
		p.assertShows(name, true)
	}
}

func TestReadoutsReplaceTheDueDate(t *testing.T) {
	withReadout := leadRow("a", "Alpha", neutralLead("ellipsis"), "the detail", counted("checkmark", task.ToneSuccess, "1/1"))
	withReadout.Due = day(1)
	without := item("b", "Bravo", 2)
	without.Detail = "other detail"
	p := newPanel(t, withReadout, without)
	p.tt.Frame()

	p.assertShows("the detail", true)
	p.assertShows("due 1 Nov", false)
	p.assertShows("due 2 Nov", true)
}

func TestClockAndAlertReadoutsShowTimeJobsAndFailure(t *testing.T) {
	const longFailure = "Run the full suite (tooling/run-the-full-suite-on-everything)"
	now := time.Now()
	p := newPanel(t,
		fixedLeadRow("run", "Running", neutralLead("ellipsis"), "gadgets#1", elapsed(now.Add(-72*time.Second)), counted("circle.fill", task.ToneWarning, "6/7 jobs")),
		fixedLeadRow("unk", "Unknown jobs", neutralLead("ellipsis"), "gazebo#2", elapsed(now.Add(-151*time.Second))),
		fixedLeadRow("fail", "Failed", neutralLead("xmark"), "site#3", fixedClock("0:48"), failure(longFailure)),
		fixedLeadRow("short", "Failed short", neutralLead("xmark"), "site#4", fixedClock("3:05"), failure("go test (test)")),
		fixedLeadRow("nofail", "Failed no step", neutralLead("xmark"), "site#5", fixedClock("0:05"), counted("xmark", task.ToneDanger, "2/3 jobs")),
		fixedLeadRow("done", "Done", successLead("checkmark"), "gadgets#6", ago(now.Add(-4*time.Minute-5*time.Second)), counted("checkmark", task.ToneSuccess, "3/3 jobs")),
	)
	p.tt.Frame()

	for _, text := range []string{"6/7 jobs", "1:12", "2:31", "0:48", "3:05", "0:05", "4m ago", "3/3 jobs", "2/3 jobs", "go test (test)"} {
		p.assertShows(text, true)
	}

	truncated := p.box(longFailure)
	for _, text := range []string{"site#3", "0:48"} {
		if b := p.box(text); b.W < 20 {
			t.Errorf("%q is %v wide, squeezed by the failure", text, b.W)
		}
	}
	right := func(text string) float32 { b := p.box(text); return b.X + b.W }
	if right("0:48") > truncated.X {
		t.Errorf("time ends at %v, past the failure at %v", right("0:48"), truncated.X)
	}
	if right(longFailure) > panelWidth-10 {
		t.Errorf("failure ends at %v, off the row", right(longFailure))
	}
	if ref := right("site#3"); ref > p.box("0:48").X {
		t.Errorf("detail ends at %v, past the time at %v", ref, p.box("0:48").X)
	}
	for _, text := range []string{"3/3 jobs", "2/3 jobs", "go test (test)", longFailure} {
		if got, want := right(text), right("6/7 jobs"); got != want {
			t.Errorf("%q ends at %v, want the last readouts' shared right edge %v", text, got, want)
		}
	}
	if gap := p.box("6/7 jobs").X - right("1:12"); gap < readoutGap || gap > readoutGap+20 {
		t.Errorf("time ends %v before the jobs text, want it just left of the jobs readout", gap)
	}
	if got, want := right("2:31"), right("6/7 jobs"); got != want {
		t.Errorf("time without jobs ends at %v, want the right edge %v", got, want)
	}
}

func TestReadoutsLineUpAcrossRows(t *testing.T) {
	p := newPanel(t,
		leadRow("a", "Alpha", neutralLead("ellipsis"), "x#1", counted("checkmark", task.ToneSuccess, "3/3")),
		fixedLeadRow("done", "Done", successLead("checkmark"), "gadgets#6", ago(time.Now().Add(-4*time.Minute)), counted("checkmark", task.ToneSuccess, "5/5 jobs")))
	p.tt.Frame()
	checks, jobs := p.box("3/3"), p.box("5/5 jobs")
	if checks.X+checks.W != jobs.X+jobs.W {
		t.Errorf("first ends at %v and second at %v, want the same right edge", checks.X+checks.W, jobs.X+jobs.W)
	}
}

func TestAnElapsedReadoutCountsUpWhileThePanelIsOpen(t *testing.T) {
	p := newPanel(t)
	p.a.st.Snapshot("test", []task.Task{fixedLeadRow("run", "Running", neutralLead("ellipsis"), "gadgets#1", elapsed(p.tt.Now().Add(-72*time.Second)))})
	p.tt.Frame()
	p.assertShows("1:12", true)

	p.tt.Advance(3 * time.Second)

	p.assertShows("1:15", true)
	p.assertShows("1:12", false)
}

func TestThePanelAsksForNoFramesWhenNothingIsRunning(t *testing.T) {
	framesAfterAdvancing := func(tasks ...task.Task) int {
		p := newPanel(t, tasks...)
		frames := 0
		p.tt = ui.NewTesterWith(func(c *ui.Context) {
			frames++
			p.a.view(c)
		}, panelWidth, panelHeight, ui.TesterOptions{})
		p.tt.Frame()
		before := frames
		p.tt.Advance(5 * time.Second)
		if p.tt.FramePending() {
			t.Error("a frame is still pending")
		}
		return frames - before
	}
	now := time.Now()
	idle := framesAfterAdvancing(
		fixedLeadRow("done", "Done", successLead("checkmark"), "gadgets#1", ago(now.Add(-time.Hour)), counted("checkmark", task.ToneSuccess, "1/1 jobs")),
		fixedLeadRow("fail", "Failed", neutralLead("xmark"), "gadgets#2", fixedClock("1:00")),
		item("a", "Alpha", 1))
	if idle != 0 {
		t.Errorf("drew %d frames in 5 seconds with no clock running", idle)
	}

	running := framesAfterAdvancing(fixedLeadRow("run", "Running", neutralLead("ellipsis"), "gadgets#3", elapsed(now)))
	if running < 4 || running > 6 {
		t.Errorf("drew %d frames in 5 seconds with a clock running, want about one a second", running)
	}
}

func TestAnOrdinaryTasksDueDateSitsOnTheDetailLine(t *testing.T) {
	tk := item("a", "Alpha", 1)
	tk.Detail = "the detail"
	p := newPanel(t, tk)
	p.tt.Frame()

	title, detail, due := p.box("Alpha"), p.box("the detail"), p.box("due 1 Nov")
	if due.Y != detail.Y {
		t.Errorf("due at y=%v, detail at y=%v", due.Y, detail.Y)
	}
	if due.Y <= title.Y {
		t.Errorf("due at y=%v is not below the title at y=%v", due.Y, title.Y)
	}
	if due.X+due.W < panelWidth-20 {
		t.Errorf("due ends at x=%v, not at the row's right edge", due.X+due.W)
	}
}

func TestHoveringShowsDismissWithoutMovingTheText(t *testing.T) {
	kinds := []task.Task{
		func() task.Task { tk := item("a", "Alpha", 1); tk.Detail = "the detail"; return tk }(),
		leadRow("b", "Bravo", neutralLead("ellipsis"), "w#1", counted("checkmark", task.ToneSuccess, "1/1")),
		fixedLeadRow("c", "Charlie", successLead("checkmark"), "l#1", ago(time.Now().Add(-time.Hour)), counted("checkmark", task.ToneSuccess, "1/1 jobs")),
	}
	for _, tk := range kinds {
		t.Run(tk.Title, func(t *testing.T) {
			p := newPanel(t, tk)
			p.tt.Frame()
			if _, ok := p.tt.Access("Dismiss"); ok {
				t.Fatal("dismiss shown without hover")
			}
			before := p.box(tk.Title)
			var detailBefore ui.Rect
			if tk.Detail != "" {
				detailBefore = p.box(tk.Detail)
			}

			p.hover(tk.Title)

			dismiss, ok := p.tt.Access("Dismiss")
			if !ok {
				t.Fatal("dismiss not shown on hover")
			}
			if dismiss.Bounds.X+dismiss.Bounds.W < panelWidth-20 {
				t.Errorf("dismiss ends at x=%v, not at the row's right edge", dismiss.Bounds.X+dismiss.Bounds.W)
			}
			if after := p.box(tk.Title); before != after {
				t.Errorf("title moved from %v to %v", before, after)
			}
			if tk.Detail != "" {
				if after := p.box(tk.Detail); detailBefore != after {
					t.Errorf("detail moved from %v to %v", detailBefore, after)
				}
			}
		})
	}
}

func TestDismissingAndClickingWorkOnRowsWithLeadsAndReadouts(t *testing.T) {
	p := newPanel(t,
		leadRow("pr", "Alpha row", successLead("checkmark"), "w#1"),
		leadRow("pr2", "Bravo row", neutralLead("ellipsis"), "w#2"),
		fixedLeadRow("d", "Charlie fixed", successLead("checkmark"), "l#1", ago(time.Now())),
		fixedLeadRow("d2", "Delta fixed", neutralLead("xmark"), "l#2", fixedClock("0:00"), failure("go test (test)")))
	p.tt.Frame()

	p.dismiss("Alpha row")
	p.dismiss("Charlie fixed")
	p.assertShows("Alpha row", false)
	p.assertShows("Charlie fixed", false)
	if len(p.opened) != 0 {
		t.Errorf("opened %v by a dismissal", p.opened)
	}

	if err := p.tt.Click("Bravo row"); err != nil {
		t.Fatal(err)
	}
	if err := p.tt.Click("Delta fixed"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/pr2", "https://example.com/d2"}; !slices.Equal(want, p.opened) {
		t.Errorf("opened: want %v, got %v", want, p.opened)
	}
}

func TestSectionsKeepTheirOrderAndStayOutOfTheCount(t *testing.T) {
	p := newPanel(t,
		item("a", "Alpha", 1),
		inSection(item("z", "Zeta row", 2), "Zeta"),
		fixedLeadRow("d", "Fixed row", successLead("checkmark"), "l#1", ago(time.Now())),
		leadRow("pr", "First row", neutralLead("ellipsis"), "w#1"))
	p.tt.Frame()

	p.assertY(sectionFirst, "First row", true)
	p.assertY("First row", "Zeta", true)
	p.assertY("Zeta row", sectionFixed, true)
	p.assertY(sectionFixed, "Fixed row", true)
	p.assertCount(1)
}

func TestABadgeReplacesTheSourceMarkOnALead(t *testing.T) {
	plain := leadRow("a", "Alpha", neutralLead("ellipsis"), "x#1")
	badged := leadRow("b", "Alpha", neutralLead("ellipsis"), "x#2")
	badged.Badge = "alt"
	missing := leadRow("c", "Alpha", neutralLead("ellipsis"), "x#3")
	missing.Badge = "no-such-icon"

	for _, tc := range []struct {
		name      string
		tk        task.Task
		red, blue bool
	}{
		{"no badge shows the source mark", plain, true, false},
		{"a badge replaces the source mark", badged, false, true},
		{"an unknown badge draws no mark", missing, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPanel(t, tc.tk)
			p.tt.Frame()
			img, title := p.tt.Image(), p.box("Alpha")
			count := func(want color.NRGBA) int {
				n := 0
				for y := 0; y < panelHeight; y++ {
					for x := 0; x < int(title.X); x++ {
						if color.NRGBAModel.Convert(img.At(x, y)) == want {
							n++
						}
					}
				}
				return n
			}
			if got := count(markRed) > 0; got != tc.red {
				t.Errorf("source mark drawn: want %v, got %v", tc.red, got)
			}
			if got := count(markBlue) > 0; got != tc.blue {
				t.Errorf("badge drawn: want %v, got %v", tc.blue, got)
			}
		})
	}
}
