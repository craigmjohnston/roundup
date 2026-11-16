package github

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/protocol"
	"github.com/craigmjohnston/roundup/protocol/protocoltest"
	"github.com/craigmjohnston/roundup/task"
)

func serveSource(t *testing.T, src task.Source) *protocoltest.Session {
	t.Helper()
	return protocoltest.Start(t, func(ctx context.Context, in io.Reader, out io.Writer) error {
		return protocol.ServeSource(ctx, in, out, src)
	})
}

func wantHello(t *testing.T, s *protocoltest.Session, id string, sections int) {
	t.Helper()
	hello := s.Want("hello")["source"].(map[string]any)
	if hello["id"] != id || hello["name"] != "GitHub" || hello["icon"] != "github" || hello["canSignIn"] != false {
		t.Errorf("unexpected hello source %v", hello)
	}
	if icon := hello["icons"].(map[string]any)["github"].(map[string]any); icon["template"] != true {
		t.Errorf("want a template github icon, got %v", icon)
	}
	got, _ := hello["sections"].([]any)
	if len(got) != sections {
		t.Errorf("want %d sections, got %v", sections, got)
	}
}

func TestPlugin_Reviews(t *testing.T) {
	f := newFakeAPI(t)
	f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1)})
	s := serveSource(t, newSource(f, staticToken, time.Hour))

	wantHello(t, s, "github-reviews", 0)
	if got := s.Want("tasks")["tasks"].([]any); len(got) != 1 {
		t.Fatalf("want one task, got %v", got)
	}

	f.set(requestedSearch, []pullRequest{newPR(7, "sam").requestedFrom(user(me), t1), newPR(8, "sam").requestedFrom(user(me), t2)})
	s.Send("refresh")
	if got := s.Want("tasks")["tasks"].([]any); len(got) != 2 {
		t.Errorf("want two tasks after the refresh, got %v", got)
	}

	s.Close()
	s.WantExit(nil)
}

func TestPlugin_Authored(t *testing.T) {
	f := newAuthoredAPI(t)
	f.with(func() { f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour))} })
	s := serveSource(t, newAuthored(f, time.Hour))

	wantHello(t, s, "github-authored", 1)
	if got := s.Want("tasks")["tasks"].([]any); len(got) != 1 {
		t.Fatalf("want one task, got %v", got)
	}

	f.with(func() {
		f.open = []authoredPR{openAt(1, testNow.Add(-time.Hour)), openAt(2, testNow.Add(-2*time.Hour))}
	})
	s.Send("refresh")
	if got := s.Want("tasks")["tasks"].([]any); len(got) != 2 {
		t.Errorf("want two tasks after the refresh, got %v", got)
	}

	s.Close()
	s.WantExit(nil)
}
