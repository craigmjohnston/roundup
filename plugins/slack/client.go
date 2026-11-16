package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

var readOnlyMethods = map[string]bool{
	"auth.test":             true,
	"client.counts":         true,
	"conversations.history": true,
	"conversations.info":    true,
	"conversations.replies": true,
	"messages.list":         true,
	"rtm.connect":           true,
	"saved.list":            true,
	"users.conversations":   true,
	"users.info":            true,
}

type Client struct {
	baseURL  string
	http     *http.Client
	loadSess func() (session, error)
}

func NewClient(ws Workspace) *Client {
	return &Client{
		baseURL:  ws.baseURL(),
		http:     &http.Client{Timeout: time.Minute},
		loadSess: func() (session, error) { return loadSession(ws) },
	}
}

type Identity struct {
	User   string `json:"user"`
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	URL    string `json:"url"`
}

func (c *Client) WhoAmI(ctx context.Context) (Identity, error) {
	body, err := c.Call(ctx, "auth.test", nil)
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(body, &id); err != nil {
		return Identity{}, fmt.Errorf("decoding auth.test: %w", err)
	}
	return id, nil
}

// Call runs a read-only Web API method as the stored session and returns the JSON response.
func (c *Client) Call(ctx context.Context, method string, params url.Values) ([]byte, error) {
	if !readOnlyMethods[method] {
		return nil, fmt.Errorf("method %q is not on the read-only list", method)
	}
	sess, err := c.loadSess()
	if errors.Is(err, errNoSession) {
		return nil, task.ErrSignedOut
	}
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set("token", sess.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", sessionCookieName+"="+sess.Cookie)
	req.Header.Set("User-Agent", userAgent)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", method, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s response: %w", method, err)
	}

	var status struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("%s returned HTTP %d that is not JSON: %w", method, res.StatusCode, err)
	}
	switch {
	case status.OK:
		return body, nil
	case status.Error == "invalid_auth" || status.Error == "not_authed" || status.Error == "token_revoked":
		return nil, task.ErrSignedOut
	default:
		return nil, fmt.Errorf("%s: %s", method, status.Error)
	}
}
