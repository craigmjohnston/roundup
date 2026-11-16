package github

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const (
	apiBase   = "https://api.github.com"
	pollEvery = 15 * time.Second
)

//go:embed github.svg
var githubSVG []byte

// githubInfo describes a source in this package: the GitHub mark, drawn in the text colour, as its icon.
func githubInfo(id string, sections ...task.Section) task.Info {
	return task.Info{
		ID:       id,
		Name:     "GitHub",
		Icon:     "github",
		Icons:    map[string]task.Icon{"github": {SVG: githubSVG, Template: true}},
		Sections: sections,
	}
}

var errUnauthorized = errors.New("github rejected the token")

var errNoToken = errors.New("github: gh is missing or not signed in; run `gh auth login`")

// conn is the GitHub access and polling loop shared by the sources in this package.
type conn struct {
	baseURL  string
	token    func(ctx context.Context) (string, error)
	interval time.Duration
	client   *http.Client
	now      func() time.Time
	refresh  chan struct{}
}

func newConn() conn {
	return conn{baseURL: apiBase, token: ghToken, interval: pollEvery, client: &http.Client{Timeout: 30 * time.Second}, now: time.Now, refresh: make(chan struct{}, 1)}
}

// Refresh asks run to poll now. It never blocks, and requests made while a poll is running or pending become one poll.
// A request made before run starts is dropped, because run polls straight away.
func (c *conn) Refresh() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// run polls until ctx ends, then returns nil.
// It keeps the last snapshot when a poll fails and returns an error when gh has no token or GitHub rejects it.
func (c *conn) run(ctx context.Context, poll func(ctx context.Context, token string) ([]task.Task, error), update func([]task.Task)) error {
	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	var last []task.Task
	published := false
	select {
	case <-c.refresh:
	default:
	}
	for {
		wait := c.interval
		rateLimited := false
		tasks, err := poll(ctx, token)
		var limited *rateLimitedError
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, errUnauthorized):
			return err
		case errors.As(err, &limited):
			wait = max(wait, limited.wait)
			rateLimited = true
		case err == nil:
			if !published || !sameTasks(last, tasks) {
				update(tasks)
			}
			published = true
			last = tasks
		}
		if !c.sleep(ctx, wait, rateLimited) {
			return nil
		}
	}
}

// sleep waits out the interval, ending early on Refresh unless the wait is a rate limit. It reports false when ctx ends.
func (c *conn) sleep(ctx context.Context, wait time.Duration, rateLimited bool) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		case <-c.refresh:
			if !rateLimited {
				return true
			}
		}
	}
}

type rateLimitedError struct{ wait time.Duration }

func (e *rateLimitedError) Error() string {
	return "github rate limit reached, retrying in " + e.wait.String()
}

func (c *conn) graphQL(ctx context.Context, token, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("calling github: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError(resp); err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	for _, e := range envelope.Errors {
		if e.Type == "RATE_LIMITED" {
			return &rateLimitedError{wait: retryAfter(resp.Header, time.Now())}
		}
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("github query failed: %s", envelope.Errors[0].Message)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decoding data: %w", err)
	}
	return nil
}

func statusError(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden:
		return &rateLimitedError{wait: retryAfter(resp.Header, time.Now())}
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("github answered %s", resp.Status)
	}
	return nil
}

// retryAfter is how long GitHub asks callers to wait, from Retry-After or the rate-limit reset time.
func retryAfter(h http.Header, now time.Time) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil {
		return time.Duration(s) * time.Second
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if s, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return max(0, time.Unix(s, 0).Sub(now))
		}
	}
	return 0
}

func ghToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	token := strings.TrimSpace(string(out))
	if err != nil || token == "" {
		return "", errNoToken
	}
	return token, nil
}
