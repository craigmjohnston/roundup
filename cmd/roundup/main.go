package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/config"
	"github.com/craigmjohnston/roundup/external"
	"github.com/craigmjohnston/roundup/install"
	"github.com/craigmjohnston/roundup/task"
	"github.com/egoist/mygo/plugins/sfsymbols"
)

//go:embed close.svg
var closeSVG []byte

var closeIcon = ui.MustParseSVG(closeSVG)

const (
	iconSize      = 14
	leadSize      = 28
	tileIconSize  = 14
	badgeSize     = 15
	badgeIconSize = 11
	badgeOutset   = 3
)

type app struct {
	sources     []task.Source
	openURL     func(string) error
	closed      func()
	st          state
	tray        *mygo.Tray
	panel       *mygo.Window
	avatars     *avatarStore
	bitmaps     map[string]*ui.Bitmap
	icons       map[[2]string]*parsedIcon
	configHint  string
	list        ui.ListState
	sectionList ui.ListState
	fixedList   ui.ListState
	panelHeight int
	trayShown   trayShown
	sortIndex   int
	scrollTop   bool
}

func main() {
	configPath := flag.String("config", "", "configuration file (default ~/.config/roundup/config.json)")
	flag.Parse()
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	a, err := newApp(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("Roundup")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func newApp(configPath string) (*app, error) {
	if configPath == "" {
		var err error
		if configPath, err = config.DefaultPath(); err != nil {
			return nil, err
		}
	}
	cfg, _, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	a := &app{openURL: mygo.Shell.OpenExternal, configHint: configHint(configPath)}
	a.useSources(buildSources(cfg)...)
	a.useAvatars()
	path, err := dismissalsPath()
	if err != nil {
		log.Print("dismissals: ", err)
		return a, nil
	}
	a.useDismissals(path)
	return a, nil
}

// buildSources starts the configured programs together, so startup takes as long as the slowest hello.
// A program that fails to start is logged and left out; the rest keep their configured order.
// A package source is installed with go install first, inside its own concurrent open.
func buildSources(cfg config.Config) []task.Source {
	installer := sync.OnceValues(func() (*install.Installer, error) {
		dir, err := install.DefaultDir()
		if err != nil {
			return nil, err
		}
		return install.New(dir), nil
	})
	return openSources(cfg.Sources, func(src config.Source) (task.Source, error) {
		command := src.Command
		if src.Package != "" {
			inst, err := installer()
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			bin, err := inst.Resolve(ctx, src.Package)
			cancel()
			if err != nil {
				return nil, err
			}
			command = append([]string{bin}, src.Args...)
		}
		return external.Open(context.Background(), command)
	})
}

// installTimeout bounds a go install, which can stall on the network and holds up startup.
const installTimeout = 5 * time.Minute

func openSources(configured []config.Source, open func(src config.Source) (task.Source, error)) []task.Source {
	opened := make([]task.Source, len(configured))
	var wg sync.WaitGroup
	for i, ext := range configured {
		wg.Go(func() {
			src, err := open(ext)
			if err != nil {
				log.Printf("source %s: %v", ext.Name(), err)
				return
			}
			opened[i] = src
		})
	}
	wg.Wait()
	var sources []task.Source
	for _, src := range opened {
		if src != nil {
			sources = append(sources, src)
		}
	}
	return sources
}

// configHint is the line shown when no sources are configured.
func configHint(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(filepath.Separator)) {
		path = "~" + path[len(home):]
	}
	return "Add sources in " + path + "."
}

func (a *app) useSources(sources ...task.Source) {
	a.sources = sources
	a.st.noSources = len(sources) == 0
	for _, src := range sources {
		a.st.AddSource(src.Info())
	}
}

func (a *app) useAvatars() {
	dir, err := defaultAvatarDir()
	if err != nil {
		log.Print("avatars: ", err)
	}
	a.avatars = newAvatarStore(defaultAvatarClient(), dir, func() { mygo.RunOnMain(a.refresh) })
}

func (a *app) useDismissals(path string) {
	a.st.dismissedPath = path
	a.st.dismissed = loadDismissals(path)
}

func (a *app) ready() {
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{
		Icon:           trayIcon(),
		IconIsTemplate: true,
		ToolTip:        "Roundup",
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	a.tray.OnClick(a.toggle)
	a.tray.OnAppearanceChange(a.updateTray)
	a.tray.OnRightClick(func() {
		a.closePanel()
		a.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleQuit}}))
		defer a.tray.SetMenu(nil)
		a.tray.PopUpMenu()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.tray.Destroy() })
	mygo.Power.OnResume(func() { refreshSources(a.sources) })

	if len(a.sources) > 0 {
		go a.spinUntilLoaded()
	}
	for _, src := range a.sources {
		go a.runSource(src)
	}
}

// spinUntilLoaded animates the tray icon until a source has answered, with tasks or a signed-out session.
func (a *app) spinUntilLoaded() {
	frames := spinnerFrames()
	tick := time.NewTicker(90 * time.Millisecond)
	defer tick.Stop()
	for i := 0; ; i++ {
		done := false
		mygo.RunOnMain(func() {
			done = a.st.loadedAny() || len(a.st.SignedOutIDs()) > 0
			if done {
				a.updateTray()
				return
			}
			if err := a.tray.SetIcon(frames[i%len(frames)], true); err != nil {
				log.Print("tray icon: ", err)
			}
		})
		if done {
			return
		}
		<-tick.C
	}
}

func (a *app) runSource(src task.Source) {
	name := src.Info().ID
	err := src.Run(context.Background(), func(tasks []task.Task) {
		mygo.RunOnMain(func() {
			if err := a.st.Snapshot(name, tasks); err != nil {
				log.Print("dismissals: ", err)
			}
			a.refresh()
		})
	})
	switch {
	case errors.Is(err, task.ErrSignedOut):
		mygo.RunOnMain(func() {
			a.st.SignedOut(name)
			a.refresh()
		})
	case err != nil:
		log.Print("source: ", err)
	}
}

func (a *app) signIn(src task.Source, signer task.SignIn) {
	name := src.Info().ID
	if a.st.source(name).signingIn {
		return
	}
	a.st.SignInStarted(name)
	a.refresh()
	go func() {
		err := signer.SignIn(context.Background())
		mygo.RunOnMain(func() {
			if err != nil {
				a.st.SignInFailed(name, err)
			} else {
				a.st.SignedIn(name)
				go a.runSource(src)
			}
			a.refresh()
		})
	}()
}

func (a *app) refresh() {
	a.updateTray()
	if a.panel != nil {
		if before := a.panelHeight; a.fitHeight() != before {
			a.panel.SetSize(flyoutWidth, a.panelHeight)
		}
		a.panel.Invalidate()
	}
}

// updateTray redraws the menu bar readout when what it shows, the menu bar's appearance, or the display it sits on has changed.
func (a *app) updateTray() {
	if a.tray == nil || !(a.st.loadedAny() || len(a.st.SignedOutIDs()) > 0) {
		return
	}
	bounds := a.tray.Bounds()
	scale := float32(mygo.Screen.PrimaryDisplay().ScaleFactor)
	if bounds.Width > 0 {
		scale = float32(mygo.Screen.DisplayMatching(bounds).ScaleFactor)
	}
	if scale <= 0 {
		scale = 2
	}
	shown := trayShown{readout: a.st.Readout(), dark: a.tray.Dark(), height: readoutHeightFor(bounds.Height), scale: scale}
	if shown == a.trayShown {
		return
	}
	icon, highlighted, err := readoutIcons(shown.readout, shown.dark, shown.height, shown.scale)
	if err == nil {
		err = a.tray.SetIcon(icon, false)
	}
	if err == nil {
		err = a.tray.SetHighlightedIcon(highlighted)
	}
	if err != nil {
		log.Print("tray icon: ", err)
		return
	}
	a.trayShown = shown
}

type trayShown struct {
	readout readout
	dark    bool
	height  int
	scale   float32
}

// fitHeight records and returns the height the panel needs for its content.
func (a *app) fitHeight() int {
	a.panelHeight = int(a.st.layout().total)
	return a.panelHeight
}

func (a *app) closePanel() {
	if a.closed != nil {
		a.closed()
	}
	if a.panel != nil {
		a.panel.Close()
	}
}

func (a *app) open(tk task.Task) {
	if err := a.openURL(tk.URL); err != nil {
		log.Print(err)
	}
	a.closePanel()
}

func (a *app) dismiss(tk task.Task) {
	if err := a.st.Dismiss(tk); err != nil {
		log.Print("dismissals: ", err)
	}
	a.refresh()
}

func (a *app) toggle() {
	if a.panel != nil {
		a.closePanel()
		return
	}
	a.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:         a.tray,
		Placement:    mygo.PlacementBottom,
		Gap:          6,
		Width:        flyoutWidth,
		Height:       a.fitHeight(),
		Focusable:    true,
		Shadow:       true,
		CornerRadius: 12,
		Content:      ui.View(a.view),
	})
	a.panel.OnClosed(func() { a.panel = nil })
	refreshSources(a.sources)
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	lay := a.st.layout()
	ui.Column(c).Fill().Background(t.Surface).Children(func() {
		a.header(c)
		ui.Divider(c)
		for _, src := range a.sources {
			if a.st.source(src.Info().ID).signedOut {
				a.signedOutNotice(c, src)
				ui.Divider(c)
			}
		}
		switch a.st.Mode() {
		case modeList:
			if a.scrollTop {
				a.list.ScrollTo(0, ui.Start)
				a.scrollTop = false
			}
			a.list.Key = func(i int) any {
				if i >= len(a.st.tasks) {
					return nil
				}
				return [2]string{a.st.tasks[i].Source, a.st.tasks[i].Key}
			}
			ui.List(c, &a.list, len(a.st.tasks)).Padding(6).Height(lay.main).Shrink(0).Rows(func(r ui.ListRow) {
				a.taskRow(r, a.st.tasks[r.Index])
			})
		case modeNoSources:
			ui.Column(c).Height(lay.main).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Padding(0, 24).Children(func() {
				ui.Text(c, a.configHint).TextColor(t.TextMuted).TextAlign(ui.Center)
			})
		case modeEmpty:
			ui.Column(c).Height(lay.main).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "No tasks").TextColor(t.TextMuted)
			})
		}
		a.sectionArea(c, "sections", &a.sectionList, a.st.scrollingRows(), lay.sections)
		a.sectionArea(c, "fixed", &a.fixedList, a.st.fixedRows(), lay.fixed)
	})
}

func (a *app) sectionArea(c *ui.Context, key string, state *ui.ListState, rows []row, height float32) {
	if len(rows) == 0 {
		return
	}
	ui.Divider(c)
	state.Key = func(i int) any {
		if i >= len(rows) {
			return nil
		}
		return [2]string{rows[i].task.Source + rows[i].heading, rows[i].task.Key}
	}
	ui.List(c.Key(key), state, len(rows)).Padding(0, 6, 6).Height(height).Shrink(0).Rows(func(r ui.ListRow) {
		if rw := rows[r.Index]; rw.heading != "" {
			a.heading(r.Context, rw.heading)
			return
		}
		a.taskRow(r, rows[r.Index].task)
	})
}

func (a *app) header(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Tasks").Bold()
		ui.Text(c, fmt.Sprint(len(a.st.tasks))).TextColor(t.TextMuted)
		ui.Spacer(c)
		a.sortMenu(c)
	})
}

var sortLabels = []string{"Due date", "Date added"}

func (a *app) sortMenu(c *ui.Context) {
	t := c.Theme()
	trigger := ui.Row(c.Key("sort")).Padding(3, 6).Radius(5).Gap(5).AlignItems(ui.Center).Label("Sort by")
	if trigger.Hovered() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() {
		sfsymbols.Symbol(c, "arrow.up.arrow.down").Size(11).Color(t.TextMuted).Weight(sfsymbols.Medium)
		ui.Text(c, sortLabels[a.sortIndex]).TextColor(t.TextMuted).SingleLine()
	})
	trigger.Menu(func(m *ui.Menu) {
		for i, label := range sortLabels {
			if m.Item(label).Checked(a.sortIndex == i).Chosen() {
				a.sortIndex = i
				a.st.SetSort(sortMode(i))
				a.scrollTop = true
			}
		}
	})
}

func (a *app) signedOutNotice(c *ui.Context, src task.Source) {
	t := c.Theme()
	info := src.Info()
	ss := a.st.source(info.ID)
	signer, canSignIn := src.(task.SignIn)
	ui.Column(c).Padding(8, 16).Gap(4).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			a.sourceIcon(c, info.ID, iconSize)
			ui.Text(c, info.Name+" is signed out").FontSize(12).Grow(1).SingleLine()
			if !canSignIn {
				return
			}
			label := "Sign in"
			if ss.signingIn {
				label = "Signing in…"
			}
			ui.Button(c.Key("signin-"+info.ID), label).Disabled(ss.signingIn).OnClick(func() { a.signIn(src, signer) })
		})
		if ss.signInErr != "" {
			ui.Text(c, ss.signInErr).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

// leading draws a row's 28-point round lead: the person's photo with the source badge, or the source on a tile.
func (a *app) leading(c *ui.Context, tk task.Task, rowBackground ui.Color) {
	t := c.Theme()
	if tk.Person == "" {
		ui.Row(c).Size(leadSize, leadSize).Radius(leadSize / 2).Background(t.SurfaceHover).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			a.sourceIcon(c, tk.Source, tileIconSize)
		})
		return
	}
	ui.Box(c).Size(leadSize, leadSize).Children(func() {
		ui.Avatar(c, tk.Person, a.photo(tk.AvatarURL)).Size(leadSize, leadSize)
		a.cornerBadge(c, tk, rowBackground)
	})
}

// photo returns the decoded profile photo, or nil while it is loading or when it cannot be shown.
func (a *app) photo(avatarURL string) *ui.Bitmap {
	if bmp, ok := a.bitmaps[avatarURL]; ok {
		return bmp
	}
	data := a.avatars.Get(avatarURL)
	if data == nil {
		return nil
	}
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		bmp = nil
	}
	if a.bitmaps == nil {
		a.bitmaps = map[string]*ui.Bitmap{}
	}
	a.bitmaps[avatarURL] = bmp
	return bmp
}

func (a *app) heading(c *ui.Context, name string) {
	t := c.Theme()
	ui.Text(c, name).FontSize(11).Bold().TextColor(t.TextMuted).SingleLine().Padding(14, 10, 4)
}

func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := color.NRGBA{A: 255}
	for y := range 32 {
		for x := range 32 {
			dx, dy := float64(x)-15.5, float64(y)-15.5
			d := dx*dx + dy*dy
			if d < 13*13 && d > 10*10 || y >= 14 && y <= 17 && x >= 9 && x <= 22 {
				img.Set(x, y, ink)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
