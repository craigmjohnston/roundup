package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/coder/websocket"
)

var (
	stateEvent = regexp.MustCompile(`mark|saved|later|star|badge|count|hello|error|goodbye|reconnect`)
	urlInError = regexp.MustCompile(`(?:https?|wss?)://[^\s"]+`)
)

// Listen opens the live event connection Slack's own client uses and prints each event's type until d passes.
func Listen(ctx context.Context, ws Workspace, d time.Duration, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	conn, err := NewClient(ws).connectEvents(ctx)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	for {
		_, data, err := conn.Read(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading event: %w", err)
		}
		fmt.Fprintln(out, time.Now().Format("15:04:05"), describeEvent(data))
	}
}

// connectEvents opens the event websocket; the connection URL carries a ticket, so errors never include it.
func (c *Client) connectEvents(ctx context.Context) (*websocket.Conn, error) {
	body, err := c.Call(ctx, "rtm.connect", url.Values{"batch_presence_aware": {"1"}})
	if err != nil {
		return nil, err
	}
	var connect struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &connect); err != nil {
		return nil, fmt.Errorf("decoding rtm.connect: %w", err)
	}
	sess, err := c.loadSess()
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.Dial(ctx, connect.URL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Cookie":     {sessionCookieName + "=" + sess.Cookie},
		"User-Agent": {userAgent},
		"Origin":     {"https://app.slack.com"},
	}})
	if err != nil {
		return nil, fmt.Errorf("opening event connection: %s", urlInError.ReplaceAllString(err.Error(), "<url>"))
	}
	conn.SetReadLimit(1 << 22)
	return conn, nil
}

// describeEvent prints state events in full and every other event as its type and field names, without content.
func describeEvent(data []byte) string {
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(data, &ev); err != nil {
		return fmt.Sprintf("(not JSON, %d bytes)", len(data))
	}
	var typ, subtype string
	_ = json.Unmarshal(ev["type"], &typ)
	_ = json.Unmarshal(ev["subtype"], &subtype)
	if stateEvent.MatchString(typ) {
		return string(data)
	}
	keys := make([]string, 0, len(ev))
	for k := range ev {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	label := typ
	if subtype != "" {
		label += "/" + subtype
	}
	return fmt.Sprintf("%s {%s}", label, strings.Join(keys, ","))
}
