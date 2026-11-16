package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/chromedp/cdproto/network"

	"github.com/craigmjohnston/roundup/task"
)

func TestClient_CallSendsTheSessionTokenAndCookie(t *testing.T) {
	var gotPath, gotCookie, gotToken, gotParam string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCookie = r.Header.Get("Cookie")
		gotToken = r.PostFormValue("token")
		gotParam = r.PostFormValue("filter")
		w.Write([]byte(`{"ok":true,"saved_items":[]}`))
	}))
	defer srv.Close()

	body, err := testClient(srv.URL, session{Token: "xoxc-1", Cookie: "xoxd-abc%2F"}).Call(context.Background(), "saved.list", url.Values{"filter": {"saved"}})
	if err != nil {
		t.Fatal(err)
	}

	assertEqual(t, `{"ok":true,"saved_items":[]}`, string(body))
	assertEqual(t, "/api/saved.list", gotPath)
	assertEqual(t, "d=xoxd-abc%2F", gotCookie)
	assertEqual(t, "xoxc-1", gotToken)
	assertEqual(t, "saved", gotParam)
}

func TestClient_CallRefusesMethodsOffTheReadOnlyList(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	_, err := testClient(srv.URL, session{Token: "xoxc-1", Cookie: "xoxd-1"}).Call(context.Background(), "chat.postMessage", nil)

	if err == nil {
		t.Fatal("want an error for chat.postMessage, got none")
	}
	if called {
		t.Fatal("want no request sent, got one")
	}
}

func TestClient_CallReportsAnExpiredSession(t *testing.T) {
	t.Run("when slack rejects the token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
		}))
		defer srv.Close()

		_, err := testClient(srv.URL, session{Token: "xoxc-1", Cookie: "xoxd-1"}).Call(context.Background(), "auth.test", nil)

		if !errors.Is(err, task.ErrSignedOut) {
			t.Fatalf("want ErrSignedOut, got %v", err)
		}
	})

	t.Run("when nothing is stored", func(t *testing.T) {
		c := &Client{http: http.DefaultClient, loadSess: func() (session, error) { return session{}, errNoSession }}

		_, err := c.Call(context.Background(), "auth.test", nil)

		if !errors.Is(err, task.ErrSignedOut) {
			t.Fatalf("want ErrSignedOut, got %v", err)
		}
	})
}

func TestClient_CallReturnsSlackErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL, session{Token: "xoxc-1", Cookie: "xoxd-1"}).Call(context.Background(), "client.counts", nil)

	if err == nil || err.Error() != "client.counts: ratelimited" {
		t.Fatalf("want %q, got %v", "client.counts: ratelimited", err)
	}
}

func TestTokenFromWorkspacePage(t *testing.T) {
	t.Run("finds the token in the page", func(t *testing.T) {
		var gotCookie string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotCookie = r.Header.Get("Cookie")
			w.Write([]byte(`<script>var boot = {"team_id":"T1","api_token":"xoxc-12-34-abcdef","x":1}</script>`))
		}))
		defer srv.Close()

		token, err := tokenFromWorkspacePage(context.Background(), srv.Client(), srv.URL, "xoxd-1")
		if err != nil {
			t.Fatal(err)
		}

		assertEqual(t, "xoxc-12-34-abcdef", token)
		assertEqual(t, "d=xoxd-1", gotCookie)
	})

	t.Run("fails when the page has no token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`<html>Sign in</html>`))
		}))
		defer srv.Close()

		_, err := tokenFromWorkspacePage(context.Background(), srv.Client(), srv.URL, "xoxd-1")

		if err == nil {
			t.Fatal("want an error, got none")
		}
	})
}

func TestSessionCookieFromBrowser(t *testing.T) {
	tests := []struct {
		name    string
		cookies []*network.Cookie
		want    string
	}{
		{
			name: "the slack.com session cookie",
			cookies: []*network.Cookie{
				{Name: "b", Domain: ".slack.com", Value: "xyz"},
				{Name: "d", Domain: ".slack.com", Value: "xoxd-abc"},
			},
			want: "xoxd-abc",
		},
		{
			name:    "a d cookie on another site",
			cookies: []*network.Cookie{{Name: "d", Domain: ".example.com", Value: "xoxd-abc"}},
			want:    "",
		},
		{
			name:    "a d cookie that is not a session",
			cookies: []*network.Cookie{{Name: "d", Domain: ".slack.com", Value: "pending"}},
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertEqual(t, tc.want, sessionCookieFromBrowser(tc.cookies))
		})
	}
}

func TestParseWorkspace(t *testing.T) {
	ws, err := ParseWorkspace("acme-co")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "https://acme-co.slack.com", ws.baseURL())

	for _, bad := range []string{"", "acme.slack.com", "https://acme", "a/b"} {
		if _, err := ParseWorkspace(bad); err == nil {
			t.Errorf("want an error for %q, got none", bad)
		}
	}
}

func TestLinkPasswordManager(t *testing.T) {
	const manifest = "com.1password.1password.json"

	t.Run("links the 1Password manifest into the profile", func(t *testing.T) {
		chromeDir, profileDir := t.TempDir(), t.TempDir()
		src := filepath.Join(chromeDir, "NativeMessagingHosts", manifest)
		if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(src, []byte(`{"name":"com.1password.1password"}`), 0o600); err != nil {
			t.Fatal(err)
		}

		for range 2 {
			if err := linkPasswordManager(chromeDir, profileDir); err != nil {
				t.Fatal(err)
			}
		}

		got, err := os.ReadFile(filepath.Join(profileDir, "NativeMessagingHosts", manifest))
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, `{"name":"com.1password.1password"}`, string(got))
	})

	t.Run("does nothing when 1Password is not installed", func(t *testing.T) {
		profileDir := t.TempDir()

		if err := linkPasswordManager(t.TempDir(), profileDir); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(profileDir, "NativeMessagingHosts")); !os.IsNotExist(err) {
			t.Fatalf("want no NativeMessagingHosts dir, got err %v", err)
		}
	})
}

func testClient(baseURL string, s session) *Client {
	return &Client{baseURL: baseURL, http: http.DefaultClient, loadSess: func() (session, error) { return s, nil }}
}

func assertEqual(t *testing.T, want, got string) {
	t.Helper()
	if want != got {
		t.Errorf("want %q, got %q", want, got)
	}
}
