package main

import (
	"errors"
	"image/color"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/craigmjohnston/roundup/task"
)

const photoURL = "https://avatars.example.test/jane.png"

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network is not available in tests")
}

func offlineAvatars(t *testing.T) *avatarStore {
	t.Helper()
	return newAvatarStore(&http.Client{Transport: failingTransport{}}, t.TempDir(), nil)
}

func seedAvatar(t *testing.T, s *avatarStore, url string, data []byte) {
	t.Helper()
	if err := os.WriteFile(s.path(url), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func personTask(key, title, person, avatarURL, source string) task.Task {
	tk := item(key, title, 1)
	tk.Source, tk.Person, tk.AvatarURL = source, person, avatarURL
	return tk
}

func newPhotoPanel(t *testing.T, store *avatarStore, tasks ...task.Task) *panel {
	t.Helper()
	p := newPanel(t, tasks...)
	p.a.st.AddSource(markInfo("source-a"))
	p.a.st.AddSource(markInfo("source-b"))
	p.a.avatars = store
	p.tt.Frame()
	return p
}

func (p *panel) avatar(name string) (ui.AccessInfo, bool) {
	p.t.Helper()
	for _, a := range p.tt.AccessAll() {
		if a.Role == ui.RoleImage && a.Name == name {
			return a, true
		}
	}
	return ui.AccessInfo{}, false
}

func (p *panel) hasMarkNextTo(a ui.AccessInfo) bool {
	p.t.Helper()
	img := p.tt.Image()
	cx, cy := int(a.Bounds.X+leadSize+badgeOutset-badgeSize/2), int(a.Bounds.Y+leadSize+badgeOutset-badgeSize/2)
	surface := color.NRGBAModel.Convert(img.At(panelWidth-2, panelHeight-2))
	for y := cy - 4; y <= cy+4; y++ {
		for x := cx - 4; x <= cx+4; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)) != surface {
				return true
			}
		}
	}
	return false
}

func TestARowWithAPersonAndALoadedPhotoShowsTheAvatarAndTheSourceBadge(t *testing.T) {
	store := offlineAvatars(t)
	seedAvatar(t, store, photoURL, pngBytes(t, color.RGBA{R: 200, A: 255}))
	p := newPhotoPanel(t, store,
		personTask("a", "Alpha", "Jane Doe", photoURL, "source-a"),
		personTask("b", "Bravo", "Pat Roe", photoURL, "unknown-source"))

	withMark, ok := p.avatar("Jane Doe")
	if !ok {
		t.Fatalf("avatar not drawn; texts: %q", p.tt.Texts())
	}
	if withMark.Bounds.W != leadSize || withMark.Bounds.H != leadSize {
		t.Errorf("avatar size: want %dx%d, got %vx%v", leadSize, leadSize, withMark.Bounds.W, withMark.Bounds.H)
	}
	if p.tt.HasText("JD") {
		t.Error("initials shown over a loaded photo")
	}
	if !p.hasMarkNextTo(withMark) {
		t.Error("no source mark on the badge of the row")
	}
	unknown, _ := p.avatar("Pat Roe")
	if p.hasMarkNextTo(unknown) {
		t.Error("source mark drawn for an unknown source")
	}
}

func TestARowWithAPersonAndNoPhotoShowsInitials(t *testing.T) {
	cases := map[string]string{
		"no photo address":                   "",
		"a photo still loading":              photoURL,
		"a non-https photo link":             "http://avatars.example.test/jane.png",
		"a cached file that is not an image": "https://avatars.example.test/broken.png",
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			store := offlineAvatars(t)
			seedAvatar(t, store, "https://avatars.example.test/broken.png", []byte("not an image"))
			p := newPhotoPanel(t, store, personTask("a", "Alpha", "Jane Doe", url, "source-b"))

			avatar, ok := p.avatar("Jane Doe")
			if !ok {
				t.Fatalf("avatar not drawn; texts: %q", p.tt.Texts())
			}
			if !p.tt.HasText("JD") {
				t.Errorf("initials not shown; texts: %q", p.tt.Texts())
			}
			if !p.hasMarkNextTo(avatar) {
				t.Error("no source mark on the badge")
			}
		})
	}
}

func TestARowWithoutAPersonShowsTheSourceOnATile(t *testing.T) {
	store := offlineAvatars(t)
	p := newPhotoPanel(t, store, personTask("a", "Alpha", "", "", "source-a"))

	for _, a := range p.tt.AccessAll() {
		if a.Role == ui.RoleImage {
			t.Errorf("avatar drawn on a row with no person: %+v", a)
		}
	}
	row, ok := p.tt.Access("Alpha")
	if !ok {
		t.Fatalf("row not found; texts: %q", p.tt.Texts())
	}
	img := p.tt.Image()
	x, y := int(row.Bounds.X)+10+2, int(row.Bounds.Y+row.Bounds.H/2)
	if color.NRGBAModel.Convert(img.At(x, y)) == color.NRGBAModel.Convert(img.At(panelWidth-2, panelHeight-2)) {
		t.Error("tile background not drawn at the row's lead")
	}
}

func TestTitlesLineUpAcrossPhotoInitialsAndTileRows(t *testing.T) {
	store := offlineAvatars(t)
	seedAvatar(t, store, photoURL, pngBytes(t, color.RGBA{G: 200, A: 255}))
	p := newPhotoPanel(t, store,
		personTask("a", "Alpha photo", "Jane Doe", photoURL, "source-a"),
		personTask("b", "Bravo initials", "Sam Lee", "", "source-b"),
		personTask("c", "Charlie tile", "", "", "source-a"))

	var xs []float32
	for _, title := range []string{"Alpha photo", "Bravo initials", "Charlie tile"} {
		box, ok := p.tt.Find(title)
		if !ok {
			t.Fatalf("%q not found; texts: %q", title, p.tt.Texts())
		}
		xs = append(xs, box.X)
	}
	if xs[0] != xs[1] || xs[1] != xs[2] {
		t.Errorf("title x positions differ: %v", xs)
	}
}

func TestDismissingAndClickingStillWorkOnARowWithAPhoto(t *testing.T) {
	store := offlineAvatars(t)
	seedAvatar(t, store, photoURL, pngBytes(t, color.RGBA{B: 200, A: 255}))
	p := newPhotoPanel(t, store,
		personTask("a", "Alpha", "Jane Doe", photoURL, "source-a"),
		personTask("b", "Bravo", "Jane Doe", photoURL, "source-a"))

	p.dismiss("Alpha")
	p.assertShows("Alpha", false)
	if len(p.opened) != 0 {
		t.Errorf("opened %v by a dismissal", p.opened)
	}

	if err := p.tt.Click("Bravo"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://example.com/b"}; !slices.Equal(want, p.opened) {
		t.Errorf("opened: want %v, got %v", want, p.opened)
	}
}

func TestAPhotoAppearsWhenItsDownloadFinishes(t *testing.T) {
	srv := newAvatarServer(t, servePNG(t))
	url := srv.URL + "/jane.png"
	var p *panel
	rig := newAvatarRig(t, srv, t.TempDir())
	p = newPhotoPanel(t, rig.store, personTask("a", "Alpha", "Jane Doe", url, "source-a"))
	if !p.tt.HasText("JD") {
		t.Fatalf("initials not shown while loading; texts: %q", p.tt.Texts())
	}

	rig.wait(t)
	p.tt.Frame()

	if p.tt.HasText("JD") {
		t.Error("initials still shown after the photo arrived")
	}
	if _, ok := p.avatar("Jane Doe"); !ok {
		t.Error("avatar missing after the photo arrived")
	}
}

func TestSignedOutNoticeKeepsASmallPlainSourceMark(t *testing.T) {
	a := &app{}
	a.useSources(infoSource{task.Info{ID: "source-a", Name: "Acme"}})
	a.st.SignedOut("source-a")
	tt := ui.NewTesterWith(a.view, panelWidth, panelHeight, ui.TesterOptions{})
	tt.Frame()

	box, ok := tt.Find("Acme is signed out")
	if !ok {
		t.Fatalf("notice not found; texts: %q", tt.Texts())
	}
	if want := float32(16 + iconSize + 8); box.X != want {
		t.Errorf("notice text x: want %v, got %v", want, box.X)
	}
}

func TestSignedOutNoticeOffersSignInOnlyToSourcesThatCanSignIn(t *testing.T) {
	signable := signInSource{infoSource: infoSource{task.Info{ID: "source-b", Name: "Beta"}}}
	a := &app{}
	a.useSources(infoSource{task.Info{ID: "source-a", Name: "Acme"}}, signable)
	a.st.SignedOut("source-a")
	a.st.SignedOut("source-b")
	tt := ui.NewTesterWith(a.view, panelWidth, panelHeight, ui.TesterOptions{})
	tt.Frame()

	if !tt.HasText("Acme is signed out") || !tt.HasText("Beta is signed out") {
		t.Fatalf("notices not shown; texts: %q", tt.Texts())
	}
	if _, ok := tt.Access("Sign in"); !ok {
		t.Error("no Sign in button for the source that can sign in")
	}
	buttons := 0
	for _, acc := range tt.AccessAll() {
		if acc.Role == ui.RoleButton && acc.Name == "Sign in" {
			buttons++
		}
	}
	if buttons != 1 {
		t.Errorf("want one Sign in button, got %d", buttons)
	}
}

func TestSignedOutNoticeShowsSigningInAndTheFailure(t *testing.T) {
	a := &app{}
	a.useSources(signInSource{infoSource: infoSource{task.Info{ID: "source-a", Name: "Acme"}}})
	a.st.SignedOut("source-a")
	a.st.SignInStarted("source-a")
	tt := ui.NewTesterWith(a.view, panelWidth, panelHeight, ui.TesterOptions{})
	tt.Frame()
	if !tt.HasText("Signing in…") || tt.HasText("Sign in") {
		t.Errorf("want only the Signing in… label; texts: %q", tt.Texts())
	}

	a.st.SignInFailed("source-a", errors.New("chrome closed"))
	a.refresh()
	tt.Frame()
	if !tt.HasText("chrome closed") || !tt.HasText("Sign in") {
		t.Errorf("want the failure and the Sign in label; texts: %q", tt.Texts())
	}
}
