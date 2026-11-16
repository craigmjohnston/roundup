package main

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

const (
	snapshotReviewSection = "Pull requests"
	snapshotDeploySection = "Deployments"
)

const panelWidth, panelHeight = 380, 640

const snapshotDeploysSource = "deploys"

var snapshotDeploysInfo = task.Info{
	ID:       snapshotDeploysSource,
	Name:     "Deploys",
	Icon:     "staging",
	Icons:    map[string]task.Icon{"staging": {SVG: snapshotBadge("#f5c518")}, "production": {SVG: snapshotBadge("#0fd354")}},
	Sections: []task.Section{{Name: snapshotDeploySection, Order: 1, Fixed: true}},
}

func snapshotBadge(fill string) []byte {
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7" fill="` + fill + `"/></svg>`)
}

// TestSnapshots draws the panel in memory and writes PNGs to ROUNDUP_SNAPSHOT_DIR; it opens no window.
func TestSnapshots(t *testing.T) {
	dir := os.Getenv("ROUNDUP_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set ROUNDUP_SNAPSHOT_DIR to write panel snapshots")
	}
	scenario := func(name string, build func(now time.Time) []task.Task) snapshotScenario {
		return snapshotScenario{name: name, tasks: build}
	}
	split := func(now time.Time) (mainOnly, sectionsOnly []task.Task) {
		for _, tk := range snapshotTasks(now) {
			if tk.Section == "" {
				mainOnly = append(mainOnly, tk)
			} else {
				sectionsOnly = append(sectionsOnly, tk)
			}
		}
		return mainOnly, sectionsOnly
	}
	many := func(now time.Time) []task.Task {
		mainOnly, _ := split(now)
		out := append([]task.Task{}, mainOnly...)
		for i := range 12 {
			out = append(out, task.Task{Key: fmt.Sprint("p", i), Source: "github-authored", Section: snapshotReviewSection, Title: fmt.Sprint("Pull request number ", i), Created: day(1 + i),
				Lead: &task.Status{Symbol: "ellipsis", Tone: task.ToneNeutral}, Detail: fmt.Sprint("widgets#", i),
				Readouts: []task.Readout{{Symbol: "checkmark", Tone: task.ToneSuccess, Text: "3/3"}}})
		}
		return out
	}
	scenarios := []snapshotScenario{
		scenario("list", snapshotTasks),
		scenario("many-sections", many),
		scenario("few-main-many-sections", func(now time.Time) []task.Task {
			mainOnly, _ := split(now)
			all := many(now)
			return append(append([]task.Task{}, mainOnly[:2]...), all[len(mainOnly):]...)
		}),
		scenario("many-main-few-sections", func(now time.Time) []task.Task {
			_, sectionsOnly := split(now)
			out := append([]task.Task{}, sectionsOnly...)
			for i := range 16 {
				tk := task.Task{Key: fmt.Sprint("m", i), Source: "slack-later", Title: fmt.Sprint("Saved message number ", i), Detail: "#devs · Sam Lee", Due: day(1 + i), Created: day(1)}
				if i%3 != 2 {
					tk.Person, tk.AvatarURL = "Sam Lee", snapshotPhotoURL("Sam Lee")
				}
				out = append(out, tk)
			}
			return out
		}),
		scenario("short", func(now time.Time) []task.Task {
			m, sections := split(now)
			out := append([]task.Task{}, m[:2]...)
			prs, deploys := 0, 0
			for _, tk := range sections {
				switch {
				case tk.Section == snapshotReviewSection && prs < 4:
					prs++
					out = append(out, tk)
				case tk.Section == snapshotDeploySection && deploys < 1:
					deploys++
					out = append(out, tk)
				}
			}
			return out
		}),
		scenario("no-sections", func(now time.Time) []task.Task { m, _ := split(now); return m }),
		scenario("only-sections", func(now time.Time) []task.Task { _, s := split(now); return s }),
		scenario("empty", func(time.Time) []task.Task { return nil }),
		{name: "signed-out", expired: true, tasks: func(now time.Time) []task.Task { m, _ := split(now); return m[:2] }},
	}
	for _, dark := range []bool{false, true} {
		a := &app{configHint: configHint(filepath.Join(os.Getenv("HOME"), ".config/roundup/config.json"))}
		a.useSources()
		tt := ui.NewTesterWith(a.view, panelWidth, panelHeight, ui.TesterOptions{Dark: dark})
		tt.SetSize(panelWidth, int(a.st.layout().total))
		tt.SetScale(snapshotScale())
		tt.Frame()
		writeSnapshot(t, tt, dir, "no-sources", dark)
		for _, sc := range scenarios {
			a := &app{}
			a.useSources(snapshotSources(t)...)
			a.avatars = snapshotAvatars(t)
			a.useDismissals(filepath.Join(t.TempDir(), "dismissed.json"))
			tt := ui.NewTesterWith(a.view, panelWidth, panelHeight, ui.TesterOptions{Dark: dark})
			a.st.Snapshot("fake", sc.tasks(tt.Now()))
			if sc.expired {
				a.st.SignedOut("slack-later")
			}
			tt.SetSize(panelWidth, int(a.st.layout().total))
			tt.SetScale(snapshotScale())
			tt.Frame()
			writeSnapshot(t, tt, dir, sc.name, dark)

			if sc.name != "list" {
				continue
			}
			if box, ok := tt.Find("Fix flaky checkout test"); ok {
				tt.Move(box.X+box.W/2, box.Y+box.H/2)
				writeSnapshot(t, tt, dir, "hover", dark)
			} else {
				t.Errorf("row to hover not found; texts: %q", tt.Texts())
			}
			if box, ok := tt.Find("Paginate the table widget"); ok {
				tt.Move(box.X+box.W/2, box.Y+box.H/2)
				writeSnapshot(t, tt, dir, "hover-section", dark)
			}
		}
	}
}

// snapshotSources describes the sources a person typically runs, with the icons the plugins ship.
func snapshotSources(t *testing.T) []task.Source {
	t.Helper()
	slackSVG, err := os.ReadFile("../../plugins/slack/slack.svg")
	if err != nil {
		t.Fatal(err)
	}
	githubSVG, err := os.ReadFile("../../plugins/github/github.svg")
	if err != nil {
		t.Fatal(err)
	}
	github := func(id string, sections ...task.Section) infoSource {
		return infoSource{task.Info{ID: id, Name: "GitHub", Icon: "github", Icons: map[string]task.Icon{"github": {SVG: githubSVG, Template: true}}, Sections: sections}}
	}
	slack := signInSource{infoSource: infoSource{task.Info{ID: "slack-later", Name: "Slack", Icon: "slack", Icons: map[string]task.Icon{"slack": {SVG: slackSVG}}}}, signIn: func(context.Context) error { return nil }}
	return []task.Source{slack, github("github-reviews"), github("github-authored", task.Section{Name: snapshotReviewSection}), infoSource{snapshotDeploysInfo}}
}

type snapshotScenario struct {
	name    string
	tasks   func(now time.Time) []task.Task
	expired bool
}

// infoSource is a source that only describes itself.
type infoSource struct{ info task.Info }

func (s infoSource) Info() task.Info                            { return s.info }
func (infoSource) Run(context.Context, func([]task.Task)) error { return nil }

// signInSource is an infoSource that can also sign in.
type signInSource struct {
	infoSource
	signIn func(context.Context) error
}

func (s signInSource) SignIn(ctx context.Context) error { return s.signIn(ctx) }

func writeSnapshot(t *testing.T, tt *ui.Tester, dir, name string, dark bool) {
	t.Helper()
	mode := "light"
	if dark {
		mode = "dark"
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.png", name, mode))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func snapshotPhotoURL(person string) string {
	return "https://photos.test/" + strings.ReplaceAll(strings.ToLower(person), " ", "-") + ".png"
}

func snapshotAvatars(t *testing.T) *avatarStore {
	t.Helper()
	store := offlineAvatars(t)
	for _, person := range []string{"Jane Doe", "Sam Lee", "Priya Patel", "Omar Haddad"} {
		seedAvatar(t, store, snapshotPhotoURL(person), standInPortrait(t, person))
	}
	return store
}

// standInPortrait draws a head and shoulders on a gradient tinted per person.
func standInPortrait(t *testing.T, name string) []byte {
	t.Helper()
	h := fnv.New32a()
	h.Write([]byte(name))
	hue := float64(h.Sum32()%360) / 360
	bg0, bg1 := hsl(hue, 0.45, 0.72), hsl(hue+0.06, 0.5, 0.52)
	skin := hsl(0.07+float64(h.Sum32()%7)/100, 0.45, 0.62+float64(h.Sum32()%20)/100)
	shirt := hsl(hue+0.5, 0.35, 0.3)
	const n = 96
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			f := float64(y) / n
			c := color.RGBA{uint8(float64(bg0.R)*(1-f) + float64(bg1.R)*f), uint8(float64(bg0.G)*(1-f) + float64(bg1.G)*f), uint8(float64(bg0.B)*(1-f) + float64(bg1.B)*f), 255}
			dx, dy := float64(x)-48, float64(y)-100
			if dx*dx/(40*40)+dy*dy/(42*42) < 1 {
				c = shirt
			}
			if math.Hypot(float64(x)-48, float64(y)-40) < 18 {
				c = skin
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func hsl(h, s, l float64) color.RGBA {
	h -= math.Floor(h)
	f := func(n float64) uint8 {
		k := math.Mod(n+h*12, 12)
		a := s * math.Min(l, 1-l)
		return uint8(255 * (l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))))
	}
	return color.RGBA{f(0), f(8), f(4), 255}
}

func snapshotTasks(now time.Time) []task.Task {
	checks := func(symbol string, tone task.Tone, text string) task.Readout {
		return task.Readout{Symbol: symbol, Tone: tone, Text: text}
	}
	comments := func(n int) task.Readout { return checks("bubble.left.fill", task.ToneNeutral, fmt.Sprint(n)) }
	pr := func(key, title, ref string, lead task.Status, created int, readouts ...task.Readout) task.Task {
		return task.Task{Key: key, Source: "github-authored", Section: snapshotReviewSection, Title: title, Created: day(created), URL: "https://example.com/" + key,
			Lead: &lead, Detail: ref, Readouts: readouts}
	}
	deploy := func(key, title, ref, env string, lead task.Status, outcome task.Outcome, created int, readouts ...task.Readout) task.Task {
		return task.Task{Key: key, Source: snapshotDeploysSource, Section: snapshotDeploySection, Title: title, Created: day(created), URL: "https://example.com/" + key,
			Badge: env, Lead: &lead, Detail: ref, Readouts: readouts, Outcome: outcome}
	}
	neutral := task.Status{Symbol: "ellipsis", Tone: task.ToneNeutral}
	running := task.Status{Symbol: "ellipsis", Tone: task.ToneWarning}
	failed := task.Status{Symbol: "xmark", Tone: task.ToneDanger}
	done := task.Status{Symbol: "checkmark", Tone: task.ToneSuccess}
	elapsed := func(ago time.Duration) task.Readout {
		return task.Readout{Since: now.Add(-ago), Clock: task.ClockElapsed}
	}
	alert := func(text string) task.Readout {
		return task.Readout{Symbol: "xmark", Tone: task.ToneDanger, Text: text, Alert: true}
	}
	return []task.Task{
		{Key: "s1", Source: "slack-later", Title: "Could we add a search bar for column sorting?", Detail: "#product · Jane Doe", Due: day(10), Created: day(3), Person: "Jane Doe", AvatarURL: snapshotPhotoURL("Jane Doe")},
		{Key: "s2", Source: "slack-later", Title: "Fix flaky checkout test", Detail: "#devs · Sam Lee", Due: day(12), Created: day(4), Person: "Sam Lee", AvatarURL: snapshotPhotoURL("Sam Lee")},
		{Key: "s3", Source: "slack-later", Title: "", Detail: "#support · Alex Kim", Created: day(5), Person: "Alex Kim"},
		{Key: "s5", Source: "slack-later", Title: "Onboarding call summary: Acme Ltd", Detail: "#customer-calls", Created: day(7)},
		{Key: "s4", Source: "slack-later", Title: "Reminder to send the quarterly numbers to finance before the end of the week", Detail: "Reminder", Due: day(14), Created: day(6)},
		{Key: "acme/widgets#7", Source: "github-reviews", Title: "Catch unexpected tokens in the number parser", Detail: "widgets#7 · Priya Patel", Created: day(8), Person: "Priya Patel", AvatarURL: snapshotPhotoURL("Priya Patel")},
		{Key: "acme/widgets#8", Source: "github-reviews", Title: "Notify on stuck imports", Detail: "widgets#8 · Omar Haddad · reply from priya", Created: day(9), Person: "Omar Haddad", AvatarURL: snapshotPhotoURL("Omar Haddad")},
		pr("w12", "Paginate the table widget", "widgets#12", neutral, 20, checks("checkmark", task.ToneSuccess, "12/12")),
		pr("w11", "Show a dashboard's folder in the masthead", "widgets#11", task.Status{Symbol: "plusminus", Tone: task.ToneAccent}, 19, comments(2), checks("circle.fill", task.ToneWarning, "9/12")),
		pr("w10", "Allow filtering the live agent list", "widgets#10", task.Status{Symbol: "bubble.left.fill", Tone: task.ToneAccent}, 18, comments(1), checks("checkmark", task.ToneSuccess, "12/12")),
		pr("w9", "Retry the failed import", "widgets#9", failed, 17, checks("xmark", task.ToneDanger, "10/12")),
		pr("w8", "Rename the export button", "widgets#8", done, 16, checks("checkmark", task.ToneSuccess, "4/4")),
		pr("w7", "Draft: sketch the new settings page", "widgets#7", neutral, 15, comments(3)),
		deploy("d1", "Backfill queue events newest-first", "gadgets#1248", "staging", running, "", 30, elapsed(72*time.Second), checks("circle.fill", task.ToneWarning, "6/7 jobs")),
		deploy("d2", "Declare the blueprint on resource builders", "gazebo#2933", "staging", running, "", 29, elapsed(151*time.Second)),
		deploy("d3", "Pre-import a week of calls", "gazebo#2813", "staging", failed, task.OutcomeFailed, 28, task.Readout{Text: "3:05"}, alert("go test (test)")),
		deploy("d4", "Use Link for internal links", "site#676", "production", failed, task.OutcomeFailed, 27, task.Readout{Text: "0:48"}, alert("Deploy to production (deployment-tooling/deploy-to-production)")),
		deploy("d5", "Show teams on the live agents list", "gadgets#1435", "production", done, task.OutcomeSucceeded, 26, task.Readout{Since: now.Add(-4 * time.Minute), Clock: task.ClockAgo}, checks("checkmark", task.ToneSuccess, "3/3 jobs")),
		deploy("d6", "Cache the folder lookups", "widgets#14667", "staging", done, task.OutcomeSucceeded, 25, task.Readout{Since: now.Add(-2 * time.Hour), Clock: task.ClockAgo}, checks("checkmark", task.ToneSuccess, "5/5 jobs")),
	}
}

func snapshotScale() float32 {
	if os.Getenv("ROUNDUP_SNAPSHOT_SCALE") == "1" {
		return 1
	}
	return 2
}
