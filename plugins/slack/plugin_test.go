package slack

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/craigmjohnston/roundup/protocol"
	"github.com/craigmjohnston/roundup/protocol/protocoltest"
)

func TestPlugin_SpeaksTheProtocol(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.users["U1"] = "Jane Doe"
	f.history["C1/100.1"] = msg("100.1", "U1", "review the plan")
	f.setItems(item("C1", "100.1", 2000, 1000))
	f.failWith("saved.list", "invalid_auth")
	l := f.later()
	signInErr := error(nil)
	l.authenticate = func(context.Context, Workspace) error {
		if signInErr == nil {
			f.failWith("saved.list", "")
		}
		return signInErr
	}
	s := protocoltest.Start(t, func(ctx context.Context, in io.Reader, out io.Writer) error {
		return protocol.ServeSource(ctx, in, out, l)
	})

	hello := s.Want("hello")["source"].(map[string]any)
	if hello["id"] != "slack-later" || hello["name"] != "Slack" || hello["icon"] != "slack" || hello["canSignIn"] != true {
		t.Errorf("unexpected hello source %v", hello)
	}
	if icon := hello["icons"].(map[string]any)["slack"].(map[string]any); icon["svg"] == "" {
		t.Error("the hello has no slack icon")
	}

	s.Want("signed_out")

	signInErr = errors.New("the window was closed")
	s.Send("sign_in")
	if got := s.Want("sign_in_failed")["error"]; got != "the window was closed" {
		t.Errorf("want the sign-in error, got %v", got)
	}

	signInErr = nil
	s.Send("sign_in")
	s.Want("signed_in")
	var title any
	for title != "review the plan" {
		tasks := s.Want("tasks")["tasks"].([]any)
		if len(tasks) != 1 {
			t.Fatalf("want one task, got %v", tasks)
		}
		title = tasks[0].(map[string]any)["title"]
	}

	s.Close()
	s.WantExit(nil)
}
