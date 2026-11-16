package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/zalando/go-keyring"
)

const (
	keyringService    = "slk"
	sessionCookieName = "d"
)

var (
	errNoSession    = errors.New("no stored session")
	workspaceDomain = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// Workspace is the subdomain of a Slack workspace, as in <workspace>.slack.com.
type Workspace string

func ParseWorkspace(s string) (Workspace, error) {
	if !workspaceDomain.MatchString(s) {
		return "", fmt.Errorf("workspace %q is not a slack.com subdomain", s)
	}
	return Workspace(s), nil
}

func (w Workspace) host() string    { return string(w) + ".slack.com" }
func (w Workspace) baseURL() string { return "https://" + w.host() }

type session struct {
	Token  string `json:"token"`
	Cookie string `json:"cookie"`
}

func loadSession(ws Workspace) (session, error) {
	raw, err := keyring.Get(keyringService, string(ws))
	if errors.Is(err, keyring.ErrNotFound) {
		return session{}, errNoSession
	}
	if err != nil {
		return session{}, fmt.Errorf("reading session from keychain: %w", err)
	}
	var s session
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return session{}, fmt.Errorf("decoding stored session: %w", err)
	}
	if s.Token == "" || s.Cookie == "" {
		return session{}, errNoSession
	}
	return s, nil
}

func saveSession(ws Workspace, s session) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encoding session: %w", err)
	}
	if err := keyring.Set(keyringService, string(ws), string(raw)); err != nil {
		return fmt.Errorf("writing session to keychain: %w", err)
	}
	return nil
}
