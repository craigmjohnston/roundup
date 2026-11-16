package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// Slack answers 403 to requests for workspace pages that carry no browser user agent.
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

var pageToken = regexp.MustCompile(`"api_token":"(xoxc-[A-Za-z0-9-]+)"`)

// captureSession opens Chrome on the workspace's sign-in and waits for a session cookie and client token that Slack accepts.
func captureSession(ctx context.Context, ws Workspace) (session, error) {
	profile, err := chromeProfileDir()
	if err != nil {
		return session{}, err
	}

	var sess session
	signIn := chromeSignIn{
		profileDir: profile,
		startURL:   ws.baseURL() + "/",
		ready: func(browserCtx context.Context) (bool, error) {
			res, err := chromedp.CallBrowser(browserCtx, storage.GetCookies, storage.GetCookiesParams{})
			if err != nil {
				return false, fmt.Errorf("reading cookies: %w", err)
			}
			cookie := sessionCookieFromBrowser(res.Cookies)
			if cookie == "" {
				return false, nil
			}
			token, err := tokenFromWorkspacePage(ctx, http.DefaultClient, ws.baseURL(), cookie)
			if err != nil {
				return false, err
			}
			sess = session{Token: token, Cookie: cookie}
			return true, nil
		},
	}

	fmt.Fprintf(os.Stderr, "sign in to %s in the Chrome window that just opened\n", ws.host())
	if err := signIn.run(ctx); err != nil {
		return session{}, err
	}
	return sess, nil
}

func tokenFromWorkspacePage(ctx context.Context, hc *http.Client, baseURL, cookie string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/ssb/redirect", nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Cookie", sessionCookieName+"="+cookie)
	req.Header.Set("User-Agent", userAgent)
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching workspace page: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("reading workspace page: %w", err)
	}
	m := pageToken.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("workspace page (HTTP %d, %s) has no api_token", res.StatusCode, res.Request.URL.Path)
	}
	return string(m[1]), nil
}

func sessionCookieFromBrowser(cookies []*network.Cookie) string {
	for _, c := range cookies {
		if c.Name == sessionCookieName && strings.TrimPrefix(c.Domain, ".") == "slack.com" && strings.HasPrefix(c.Value, "xoxd-") {
			return c.Value
		}
	}
	return ""
}

func chromeProfileDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding config dir: %w", err)
	}
	dir = filepath.Join(dir, "slk", "chrome-profile")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating chrome profile dir: %w", err)
	}
	return dir, nil
}

// Authenticate signs in through Chrome and stores the session in the keychain.
func Authenticate(ctx context.Context, ws Workspace) error {
	sess, err := captureSession(ctx, ws)
	if err != nil {
		return err
	}
	return saveSession(ws, sess)
}
