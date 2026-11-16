package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pngBytes(t testing.TB, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type avatarServer struct {
	*httptest.Server
	requests atomic.Int32
}

func newAvatarServer(t *testing.T, handler http.HandlerFunc) *avatarServer {
	t.Helper()
	s := &avatarServer{}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		handler(w, r)
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func servePNG(t *testing.T) http.HandlerFunc {
	body := pngBytes(t, color.RGBA{R: 200, A: 255})
	return func(w http.ResponseWriter, r *http.Request) { w.Write(body) }
}

type avatarRig struct {
	store *avatarStore
	done  chan struct{}
	dir   string
}

func newAvatarRig(t *testing.T, s *avatarServer, dir string) *avatarRig {
	t.Helper()
	r := &avatarRig{done: make(chan struct{}, 64), dir: dir}
	r.store = newAvatarStore(s.Client(), dir, func() { r.done <- struct{}{} })
	return r
}

func (r *avatarRig) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no callback after a download")
	}
}

func TestAvatarStoreDownloadsInTheBackgroundAndThenServesTheBytes(t *testing.T) {
	srv := newAvatarServer(t, servePNG(t))
	rig := newAvatarRig(t, srv, t.TempDir())
	url := srv.URL + "/a.png"

	if got := rig.store.Get(url); got != nil {
		t.Fatalf("first request returned %d bytes, want none", len(got))
	}
	rig.wait(t)

	if got := rig.store.Get(url); !bytes.Equal(pngBytes(t, color.RGBA{R: 200, A: 255}), got) {
		t.Errorf("second request returned %d bytes, want the downloaded image", len(got))
	}
	if n := srv.requests.Load(); n != 1 {
		t.Errorf("HTTP requests: want 1, got %d", n)
	}
}

func TestAvatarStoreMakesOneRequestForConcurrentAsks(t *testing.T) {
	release := make(chan struct{})
	body := pngBytes(t, color.RGBA{B: 200, A: 255})
	srv := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write(body)
	})
	rig := newAvatarRig(t, srv, t.TempDir())
	url := srv.URL + "/a.png"

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rig.store.Get(url)
		}()
	}
	wg.Wait()
	close(release)
	rig.wait(t)

	if n := srv.requests.Load(); n != 1 {
		t.Errorf("HTTP requests: want 1, got %d", n)
	}
	if rig.store.Get(url) == nil {
		t.Error("photo missing after the download")
	}
}

func TestAvatarStoreServesAnEarlierSessionsFileWithoutTheNetwork(t *testing.T) {
	srv := newAvatarServer(t, servePNG(t))
	dir := t.TempDir()
	url := srv.URL + "/a.png"
	first := newAvatarRig(t, srv, dir)
	first.store.Get(url)
	first.wait(t)

	second := newAvatarRig(t, srv, dir)
	got := second.store.Get(url)

	if got == nil {
		t.Error("second store did not serve the cached file")
	}
	if n := srv.requests.Load(); n != 1 {
		t.Errorf("HTTP requests: want 1, got %d", n)
	}
}

func TestAvatarStoreRemembersFailuresForTheSession(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"a non-200 answer": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(pngBytes(t, color.Black))
		},
		"a body that is not an image": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("<html>sign in</html>"))
		},
		"an image over 1 MB": func(w http.ResponseWriter, r *http.Request) {
			w.Write(append(pngBytes(t, color.Black), make([]byte, maxAvatarBytes)...))
		},
		"a dropped connection": func(w http.ResponseWriter, r *http.Request) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newAvatarServer(t, handler)
			dir := t.TempDir()
			rig := newAvatarRig(t, srv, dir)
			url := srv.URL + "/a.png"

			rig.store.Get(url)
			rig.wait(t)

			if got := rig.store.Get(url); got != nil {
				t.Errorf("got %d bytes after a failure", len(got))
			}
			if n := srv.requests.Load(); n != 1 {
				t.Errorf("HTTP requests: want 1, got %d", n)
			}
			if files, _ := os.ReadDir(dir); len(files) != 0 {
				t.Errorf("cache holds %d files after a failure", len(files))
			}
			select {
			case <-rig.done:
				t.Error("callback fired a second time")
			default:
			}
		})
	}
}

func TestAvatarStoreNeverFetchesNonHTTPSURLs(t *testing.T) {
	var hits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer plain.Close()
	store := newAvatarStore(plain.Client(), t.TempDir(), nil)

	for _, u := range []string{plain.URL + "/a.png", "ftp://example.com/a.png", "file:///etc/hosts", "//example.com/a.png", "not a url"} {
		if got := store.Get(u); got != nil {
			t.Errorf("Get(%q) returned bytes", u)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("plain HTTP requests: want 0, got %d", n)
	}
}

func TestAvatarStoreRunsAtMostFourDownloadsAtOnce(t *testing.T) {
	var running, peak atomic.Int32
	arrived := make(chan struct{}, 16)
	release := make(chan struct{})
	body := pngBytes(t, color.Black)
	srv := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		arrived <- struct{}{}
		<-release
		running.Add(-1)
		w.Write(body)
	})
	rig := newAvatarRig(t, srv, t.TempDir())

	const total = 10
	for i := range total {
		rig.store.Get(srv.URL + "/" + string(rune('a'+i)) + ".png")
	}
	for range maxAvatarDownloads {
		<-arrived
	}
	time.Sleep(100 * time.Millisecond)
	if n := running.Load(); n != maxAvatarDownloads {
		t.Errorf("running while saturated: want %d, got %d", maxAvatarDownloads, n)
	}
	close(release)
	for range total {
		rig.wait(t)
	}

	if p := peak.Load(); p > maxAvatarDownloads {
		t.Errorf("peak concurrent downloads: want at most %d, got %d", maxAvatarDownloads, p)
	}
	if n := srv.requests.Load(); n != total {
		t.Errorf("HTTP requests: want %d, got %d", total, n)
	}
}

func TestAvatarStoreKeepsItsFilesPrivate(t *testing.T) {
	srv := newAvatarServer(t, servePNG(t))
	dir := filepath.Join(t.TempDir(), "nested", "avatars")
	rig := newAvatarRig(t, srv, dir)
	rig.store.Get(srv.URL + "/a.png")
	rig.wait(t)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode: want 0700, got %o", got)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files: %v, err %v", files, err)
	}
	fi, _ := files[0].Info()
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode: want 0600, got %o", got)
	}
}

func TestAvatarStoreSendsNoCredentials(t *testing.T) {
	var header http.Header
	var mu sync.Mutex
	body := pngBytes(t, color.Black)
	srv := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		header = r.Header.Clone()
		mu.Unlock()
		w.Write(body)
	})
	rig := newAvatarRig(t, srv, "")
	rig.store.Get(srv.URL + "/a.png")
	rig.wait(t)

	mu.Lock()
	defer mu.Unlock()
	for _, h := range []string{"Cookie", "Authorization"} {
		if v := header.Get(h); v != "" {
			t.Errorf("%s header sent: %q", h, v)
		}
	}
}

func TestDefaultAvatarClientRefusesRedirectsToPlainHTTP(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(pngBytes(t, color.Black)) }))
	defer plain.Close()
	srv := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, plain.URL, http.StatusFound) })
	client := defaultAvatarClient()
	client.Transport = srv.Client().Transport

	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Error("redirect to plain HTTP was followed")
	}
}
