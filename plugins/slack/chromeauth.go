package slack

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const signInTimeout = 5 * time.Minute

var errSignInWindowClosed = errors.New("sign-in window closed before signing in")

type chromeSignIn struct {
	profileDir string
	startURL   string
	// ready runs once a second and reports whether the browser holds everything the caller needs.
	ready func(browserCtx context.Context) (bool, error)
}

// run opens startURL in a Chrome app window and waits for ready; returning quits that Chrome.
func (s chromeSignIn) run(ctx context.Context) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("finding config dir: %w", err)
	}
	if err := linkPasswordManager(filepath.Join(configDir, "Google", "Chrome"), s.profileDir); err != nil {
		return err
	}

	// No enable-automation flag, so Google's sign-in treats this as an ordinary Chrome window.
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx,
		chromedp.UserDataDir(s.profileDir),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
		chromedp.Flag("app", s.startURL),
		chromedp.WindowSize(520, 760),
	)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	deadline := time.After(signInTimeout)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			return fmt.Errorf("not signed in after %s", signInTimeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
		// Browser-level calls keep working after the tab closes; macOS Chrome stays running with no windows.
		targets, err := chromedp.CallBrowser(browserCtx, target.GetTargets, target.GetTargetsParams{})
		if err != nil {
			return fmt.Errorf("listing chrome windows: %w", err)
		}
		if !hasPage(targets.TargetInfos) {
			return errSignInWindowClosed
		}
		done, err := s.ready(browserCtx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// Chrome looks up the 1Password desktop app only in the profile's own NativeMessagingHosts.
func linkPasswordManager(chromeDir, profileDir string) error {
	const manifest = "com.1password.1password.json"
	src := filepath.Join(chromeDir, "NativeMessagingHosts", manifest)
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	hosts := filepath.Join(profileDir, "NativeMessagingHosts")
	if err := os.MkdirAll(hosts, 0o700); err != nil {
		return fmt.Errorf("creating native messaging dir: %w", err)
	}
	if err := os.Symlink(src, filepath.Join(hosts, manifest)); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("linking 1Password into the sign-in profile: %w", err)
	}
	return nil
}

func hasPage(targets []*target.Info) bool {
	for _, t := range targets {
		if t.Type == "page" {
			return true
		}
	}
	return false
}
