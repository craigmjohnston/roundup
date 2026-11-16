package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

type panel struct {
	t      *testing.T
	a      *app
	tt     *ui.Tester
	opened []string
	path   string
}

func newPanel(t *testing.T, tasks ...task.Task) *panel {
	t.Helper()
	return newPanelAt(t, filepath.Join(t.TempDir(), "dismissed.json"), tasks...)
}

func newPanelAt(t *testing.T, path string, tasks ...task.Task) *panel {
	t.Helper()
	p := &panel{t: t, path: path}
	p.a = &app{openURL: func(url string) error {
		p.opened = append(p.opened, url)
		return nil
	}}
	p.a.useDismissals(path)
	p.a.st.AddSource(testInfo)
	p.a.st.Snapshot("test", tasks)
	p.tt = ui.NewTesterWith(p.a.view, panelWidth, panelHeight, ui.TesterOptions{})
	return p
}

func (p *panel) hover(text string) {
	p.t.Helper()
	box, ok := p.tt.Find(text)
	if !ok {
		p.t.Fatalf("%q not found; texts: %q", text, p.tt.Texts())
	}
	p.tt.Move(box.X+box.W/2, box.Y+box.H/2)
}

func (p *panel) dismiss(text string) {
	p.t.Helper()
	p.hover(text)
	if err := p.tt.Click("Dismiss"); err != nil {
		p.t.Fatal(err)
	}
}

func (p *panel) assertCount(want int) {
	p.t.Helper()
	if got := p.a.st.TrayTitle(); got != strconv.Itoa(want) {
		p.t.Errorf("tray count: want %d, got %q", want, got)
	}
	if !p.tt.HasText(strconv.Itoa(want)) {
		p.t.Errorf("header count: want %d; texts: %q", want, p.tt.Texts())
	}
}

func (p *panel) assertShows(text string, want bool) {
	p.t.Helper()
	if got := p.tt.HasText(text); got != want {
		p.t.Errorf("shows %q: want %v, got %v; texts: %q", text, want, got, p.tt.Texts())
	}
}

func (p *panel) assertY(a, b string, aboveB bool) {
	p.t.Helper()
	boxA, okA := p.tt.Find(a)
	boxB, okB := p.tt.Find(b)
	if !okA || !okB {
		p.t.Fatalf("%q or %q not found; texts: %q", a, b, p.tt.Texts())
	}
	if (boxA.Y < boxB.Y) != aboveB {
		p.t.Errorf("%q at y=%v, %q at y=%v; want first above second: %v", a, boxA.Y, b, boxB.Y, aboveB)
	}
}

func item(key, title string, created int) task.Task {
	return task.Task{Source: "test", Key: key, Title: title, URL: "https://example.com/" + key, Created: day(created), Due: day(created)}
}

func inSection(t task.Task, section string) task.Task {
	t.Section = section
	return t
}

func manyItems(prefix string, n int, section string) []task.Task {
	var out []task.Task
	for i := range n {
		out = append(out, inSection(item(prefix+strconv.Itoa(i), prefix+" task "+strconv.Itoa(i), 1+i), section))
	}
	return out
}

func TestDismissingAMainListRow(t *testing.T) {
	p := newPanel(t, item("a", "Alpha", 1), item("b", "Bravo", 2), inSection(item("m", "Mine", 3), "Extras"))
	p.assertCount(2)

	p.dismiss("Alpha")

	p.assertShows("Alpha", false)
	p.assertShows("Bravo", true)
	p.assertCount(1)
	if len(p.opened) != 0 {
		t.Errorf("opened %v, want no link opened", p.opened)
	}
}

func TestDismissingKeepsThePanelOpen(t *testing.T) {
	p := newPanel(t, item("a", "Alpha", 1), item("b", "Bravo", 2))
	closed := 0
	p.a.closed = func() { closed++ }
	p.dismiss("Alpha")
	if closed != 0 {
		t.Errorf("panel closed %d times by a dismissal", closed)
	}
}

func TestClickingARowOpensItsLink(t *testing.T) {
	p := newPanel(t, item("a", "Alpha", 1), item("b", "Bravo", 2), inSection(item("m", "Mine", 3), "Extras"))
	closed := 0
	p.a.closed = func() { closed++ }

	if err := p.tt.Click("Bravo"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/b"}; !slices.Equal(want, p.opened) {
		t.Errorf("opened: want %v, got %v", want, p.opened)
	}
	if closed != 1 {
		t.Errorf("panel closed %d times, want 1", closed)
	}

	if err := p.tt.Click("Mine"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/b", "https://example.com/m"}; !slices.Equal(want, p.opened) {
		t.Errorf("opened: want %v, got %v", want, p.opened)
	}
}

func TestDismissingASectionRowLeavesTheCount(t *testing.T) {
	const mine = "Extras"
	p := newPanel(t, item("a", "Alpha", 1), inSection(item("m1", "Mine one", 3), mine), inSection(item("m2", "Mine two", 4), mine))

	p.dismiss("Mine one")

	p.assertShows("Mine one", false)
	p.assertShows("Mine two", true)
	p.assertShows(mine, true)
	p.assertCount(1)
	if len(p.opened) != 0 {
		t.Errorf("opened %v, want no link opened", p.opened)
	}

	p.dismiss("Mine two")
	p.assertShows(mine, false)
}

func TestADismissedTaskStaysHiddenInANewApp(t *testing.T) {
	tasks := []task.Task{item("a", "Alpha", 1), item("b", "Bravo", 2)}
	first := newPanel(t, tasks...)
	first.dismiss("Alpha")

	second := newPanelAt(t, first.path, tasks...)
	second.tt.Frame()
	second.assertShows("Alpha", false)
	second.assertShows("Bravo", true)
	second.assertCount(1)
}

func TestATaskWithANewCreatedTimeShowsAgain(t *testing.T) {
	p := newPanel(t, item("a", "Alpha", 1), item("b", "Bravo", 2))
	p.dismiss("Alpha")
	p.assertShows("Alpha", false)

	p.a.st.Snapshot("test", []task.Task{item("a", "Alpha", 5), item("b", "Bravo", 2)})
	p.tt.Frame()

	p.assertShows("Alpha", true)
	p.assertCount(2)
}

func TestScrollingTheMainListLeavesTheSectionsAlone(t *testing.T) {
	const mine = "Extras"
	tasks := append(manyItems("main", 30, ""), manyItems("mine", 3, mine)...)
	p := newPanel(t, tasks...)
	p.tt.Frame()
	before, ok := p.tt.Find("mine task 0")
	if !ok {
		t.Fatalf("section row not found; texts: %q", p.tt.Texts())
	}
	firstBefore, _ := p.tt.Find("main task 0")

	p.tt.Scroll(panelWidth/2, 150, 0, 300)

	if firstAfter, _ := p.tt.Find("main task 0"); firstAfter == firstBefore {
		t.Errorf("main list did not scroll: first row still at %v", firstAfter)
	}
	if after, _ := p.tt.Find("mine task 0"); after != before {
		t.Errorf("sections area moved: before %v, after %v", before, after)
	}
}

func TestTheSortControlReordersTheMainListOnly(t *testing.T) {
	const mine = "Extras"
	newest := item("n", "Newest due last", 9)
	newest.Due = day(20)
	oldest := item("o", "Oldest due first", 1)
	oldest.Due = day(10)
	p := newPanel(t, newest, oldest, inSection(item("s1", "Section old", 2), mine), inSection(item("s2", "Section new", 8), mine))
	p.assertY("Oldest due first", "Newest due last", true)
	p.assertY("Section new", "Section old", true)

	if err := p.tt.Click("Due date"); err != nil {
		t.Fatal(err)
	}
	if got := p.tt.Menu(); !slices.Equal(got, []string{"Due date", "Date added"}) {
		t.Fatalf("sort menu: got %q", got)
	}
	if err := p.tt.ChooseMenuItem("Date added"); err != nil {
		t.Fatal(err)
	}
	p.assertShows("Date added", true)
	p.assertShows("Due date", false)

	p.assertY("Newest due last", "Oldest due first", true)
	p.assertY("Section new", "Section old", true)
}

func TestNoSectionsAreDrawnWithoutSectionTasks(t *testing.T) {
	p := newPanel(t, item("a", "Alpha", 1))
	if p.tt.HasText("Extras") {
		t.Errorf("sections shown without section tasks; texts: %q", p.tt.Texts())
	}
}

func TestNoTasksShowsAboveTheSectionsWhenOnlySectionTasksExist(t *testing.T) {
	const mine = "Extras"
	p := newPanel(t, inSection(item("m", "Mine", 3), mine))

	p.assertShows("No tasks", true)
	p.assertY("No tasks", mine, true)
	p.assertShows("Mine", true)
	p.assertCount(0)
}

func TestNoTasksShowsOnceASourceHasDelivered(t *testing.T) {
	p := newPanel(t)
	p.assertShows("No tasks", true)

	loading := &panel{t: t, a: &app{}}
	loading.tt = ui.NewTesterWith(loading.a.view, panelWidth, panelHeight, ui.TesterOptions{})
	loading.assertShows("No tasks", false)
}

func TestThePanelSharesItsHeightBetweenTheListAndTheSections(t *testing.T) {
	const mine = "Extras"
	headingY := func(t *testing.T, p *panel) float32 {
		t.Helper()
		p.tt.Frame()
		heading, ok := p.tt.Find(mine)
		if !ok {
			t.Fatalf("heading not found; texts: %q", p.tt.Texts())
		}
		return heading.Y
	}

	t.Run("a short list takes only the room it needs and the sections get the rest", func(t *testing.T) {
		p := newPanel(t, append(manyItems("main", 3, ""), manyItems("mine", 20, mine)...)...)

		if y := headingY(t, p); y > panelHeight/2-80 {
			t.Errorf("sections start at y=%v, want them directly under three rows", y)
		}
		p.assertShows("main task 0", true)
		p.assertShows("main task 2", true)
	})

	t.Run("a long list and long sections each keep about half", func(t *testing.T) {
		p := newPanel(t, append(manyItems("main", 20, ""), manyItems("mine", 30, mine)...)...)

		if y := headingY(t, p); y < panelHeight/2-20 || y > panelHeight*2/3 {
			t.Errorf("sections start at y=%v, want about half of %d", y, panelHeight)
		}
	})

	t.Run("short sections sit at the bottom under a long list", func(t *testing.T) {
		p := newPanel(t, append(manyItems("main", 20, ""), manyItems("mine", 2, mine)...)...)

		if y := headingY(t, p); y < panelHeight*2/3 {
			t.Errorf("sections start at y=%v, want them near the bottom of %d", y, panelHeight)
		}
		p.assertShows("mine task 0", true)
		p.assertShows("mine task 1", true)
	})
}

func TestThePanelNamesTheConfigFileWhenNoSourcesAreConfigured(t *testing.T) {
	hint := configHint("/etc/roundup/config.json")
	a := &app{configHint: hint}
	a.useSources()
	tt := ui.NewTesterWith(a.view, panelWidth, panelHeight, ui.TesterOptions{})
	p := &panel{t: t, a: a, tt: tt}

	p.assertShows(hint, true)
	p.assertShows("No tasks", false)
}

func TestConfigHintAbbreviatesTheHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if want, got := "Add sources in ~/c/config.json.", configHint(filepath.Join(home, "c", "config.json")); want != got {
		t.Errorf("want %q, got %q", want, got)
	}
}
