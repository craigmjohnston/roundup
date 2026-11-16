package github

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const me = "jane"

var (
	t1 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	t2 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	t3 = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
)

func newPR(number int, author string) pullRequest {
	pr := pullRequest{ID: "PR_" + itoa(number), Number: number, Title: "Fix the thing", URL: "https://github.com/acme/widgets/pull/" + itoa(number), UpdatedAt: t3, Author: &actor{Typename: "User", Login: author, AvatarURL: avatar(author)}}
	pr.Repository.NameWithOwner = "acme/widgets"
	return pr
}

func avatar(login string) string { return "https://avatars.test/" + login }

func itoa(n int) string { return string(rune('0'+n/10)) + string(rune('0'+n%10)) }

func (pr pullRequest) requestedFrom(person reviewer, at time.Time) pullRequest {
	pr.ReviewRequests.Nodes = append(pr.ReviewRequests.Nodes, struct {
		RequestedReviewer *reviewer `json:"requestedReviewer"`
	}{&person})
	pr.TimelineItems.Nodes = append(pr.TimelineItems.Nodes, struct {
		CreatedAt         time.Time `json:"createdAt"`
		RequestedReviewer *reviewer `json:"requestedReviewer"`
	}{at, &person})
	return pr
}

func (pr pullRequest) withReview(state string) pullRequest {
	pr.ViewerLatestReview = &struct {
		State string `json:"state"`
	}{state}
	return pr
}

type commentSpec struct {
	by  string
	at  time.Time
	url string
}

func thread(resolved bool, comments ...commentSpec) reviewThread {
	th := reviewThread{IsResolved: resolved}
	for _, c := range comments {
		th.Comments.Nodes = append(th.Comments.Nodes, comment{URL: c.url, CreatedAt: c.at, Author: &actor{Typename: "User", Login: c.by, AvatarURL: avatar(c.by)}})
	}
	last := comments[len(comments)-1]
	th.LastComment.Nodes = []comment{{URL: last.url, CreatedAt: last.at, Author: &actor{Typename: "User", Login: last.by, AvatarURL: avatar(last.by)}}}
	return th
}

func (pr pullRequest) withThreads(ths ...reviewThread) pullRequest {
	pr.ReviewThreads.Nodes = append(pr.ReviewThreads.Nodes, ths...)
	return pr
}

func user(login string) reviewer { return reviewer{Typename: "User", Login: login} }

func TestBuildTasks_ReviewRequests(t *testing.T) {
	t.Run("a personal review request is a task", func(t *testing.T) {
		pr := newPR(7, "sam").requestedFrom(user(me), t1)

		want := []task.Task{{
			Key: "acme/widgets#7", Source: "github-reviews", Title: "Fix the thing",
			Detail: "widgets#7 · sam", URL: "https://github.com/acme/widgets/pull/07", Created: t1,
			Person: "sam", AvatarURL: avatar("sam"),
		}}
		got := buildTasks(me, nil, []pullRequest{pr}, nil)
		assertTasks(t, want, got)
	})

	t.Run("created is the latest request to the user, not to others", func(t *testing.T) {
		pr := newPR(7, "sam").requestedFrom(user(me), t1).requestedFrom(user("lee"), t3).requestedFrom(user(me), t2)

		got := buildTasks(me, nil, []pullRequest{pr}, nil)
		assertTasks(t, []time.Time{t2}, createds(got))
	})

	t.Run("created falls back to the updated time without a request event", func(t *testing.T) {
		pr := newPR(7, "sam").requestedFrom(user(me), t1)
		pr.TimelineItems.Nodes = nil

		got := buildTasks(me, nil, []pullRequest{pr}, nil)
		assertTasks(t, []time.Time{t3}, createds(got))
	})

	t.Run("a team request alone is not a task", func(t *testing.T) {
		pr := newPR(7, "sam").requestedFrom(reviewer{Typename: "Team"}, t1)

		if got := buildTasks(me, nil, []pullRequest{pr}, nil); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})

	t.Run("a request on the user's own pull request is not a task", func(t *testing.T) {
		pr := newPR(7, me).requestedFrom(user(me), t1)

		if got := buildTasks(me, nil, []pullRequest{pr}, nil); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})
}

func TestBuildTasks_Replies(t *testing.T) {
	mine := commentSpec{by: me, at: t1, url: "https://github.com/acme/widgets/pull/07#r1"}
	theirs := commentSpec{by: "sam", at: t2, url: "https://github.com/acme/widgets/pull/07#r2"}

	t.Run("an unanswered reply in a thread the user commented in is a task with the reply's url and time", func(t *testing.T) {
		pr := newPR(7, "lee").withReview("COMMENTED").withThreads(thread(false, mine, theirs))

		want := []task.Task{{
			Key: "acme/widgets#7", Source: "github-reviews", Title: "Fix the thing",
			Detail: "widgets#7 · lee · reply from sam", URL: theirs.url, Created: t2,
			Person: "sam", AvatarURL: avatar("sam"),
		}}
		assertTasks(t, want, buildTasks(me, nil, nil, []pullRequest{pr}))
	})

	t.Run("the newest unanswered reply across threads is used", func(t *testing.T) {
		newer := commentSpec{by: "kim", at: t3, url: "https://github.com/acme/widgets/pull/07#r3"}
		pr := newPR(7, "lee").withThreads(thread(false, mine, theirs), thread(false, mine, newer))

		got := buildTasks(me, nil, nil, []pullRequest{pr})
		assertTasks(t, []string{newer.url}, urls(got))
	})

	t.Run("the user answering last closes the thread", func(t *testing.T) {
		answer := commentSpec{by: me, at: t3, url: "u"}
		pr := newPR(7, "lee").withThreads(thread(false, mine, theirs, answer))

		if got := buildTasks(me, nil, nil, []pullRequest{pr}); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})

	t.Run("a resolved thread is ignored", func(t *testing.T) {
		pr := newPR(7, "lee").withThreads(thread(true, mine, theirs))

		if got := buildTasks(me, nil, nil, []pullRequest{pr}); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})

	t.Run("a thread the user never commented in is ignored", func(t *testing.T) {
		first := commentSpec{by: "kim", at: t1, url: "a"}
		pr := newPR(7, "lee").withThreads(thread(false, first, theirs))

		if got := buildTasks(me, nil, nil, []pullRequest{pr}); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})

	t.Run("an approval by the user closes every reply", func(t *testing.T) {
		pr := newPR(7, "lee").withReview("APPROVED").withThreads(thread(false, mine, theirs))

		if got := buildTasks(me, nil, nil, []pullRequest{pr}); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})

	t.Run("a reply on the user's own pull request is not a task", func(t *testing.T) {
		pr := newPR(7, me).withThreads(thread(false, mine, theirs))

		if got := buildTasks(me, nil, nil, []pullRequest{pr}); len(got) != 0 {
			t.Errorf("want no tasks, got %v", got)
		}
	})
}

func TestBuildTasks_BothRules(t *testing.T) {
	t.Run("a pull request matching both rules is one task, with the request's fields", func(t *testing.T) {
		mine := commentSpec{by: me, at: t1, url: "a"}
		theirs := commentSpec{by: "sam", at: t2, url: "b"}
		req := newPR(7, "lee").requestedFrom(user(me), t1)
		rep := newPR(7, "lee").withThreads(thread(false, mine, theirs))

		want := []task.Task{{
			Key: "acme/widgets#7", Source: "github-reviews", Title: "Fix the thing",
			Detail: "widgets#7 · lee", URL: "https://github.com/acme/widgets/pull/07", Created: t1,
			Person: "lee", AvatarURL: avatar("lee"),
		}}
		assertTasks(t, want, buildTasks(me, nil, []pullRequest{req}, []pullRequest{rep}))
	})
}

func assertTasks[T any](t *testing.T, want, got []T) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if !equalValue(want[i], got[i]) {
			t.Errorf("item %d: want %v, got %v", i, want[i], got[i])
		}
	}
}

func equalValue(a, b any) bool {
	switch x := a.(type) {
	case task.Task:
		return sameTasks([]task.Task{x}, []task.Task{b.(task.Task)})
	case time.Time:
		return x.Equal(b.(time.Time))
	}
	return a == b
}

func createds(tasks []task.Task) []time.Time {
	var out []time.Time
	for _, tk := range tasks {
		out = append(out, tk.Created)
	}
	return out
}

func urls(tasks []task.Task) []string {
	var out []string
	for _, tk := range tasks {
		out = append(out, tk.URL)
	}
	return out
}

func TestFresh(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	active := newPR(1, "sam")
	active.UpdatedAt = now.Add(-13 * 24 * time.Hour)
	onTheLine := newPR(2, "sam")
	onTheLine.UpdatedAt = now.Add(-staleAfter)
	stale := newPR(3, "sam")
	stale.UpdatedAt = now.Add(-staleAfter - time.Second)
	in := []pullRequest{active, onTheLine, stale}

	got := fresh(in, now)

	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Fatalf("want pull requests 1 and 2, got %v", got)
	}
	if len(in) != 3 {
		t.Fatalf("want the input left alone, got %d pull requests", len(in))
	}
}

func TestBuildTasks_Person(t *testing.T) {
	mine := commentSpec{by: me, at: t1, url: "a"}

	t.Run("a review request is from the pull request's author", func(t *testing.T) {
		pr := newPR(7, "sam").requestedFrom(user(me), t1)

		got := buildTasks(me, nil, []pullRequest{pr}, nil)

		assertEqual(t, "sam", got[0].Person)
		assertEqual(t, "https://avatars.test/sam", got[0].AvatarURL)
	})

	t.Run("a reply is from whoever wrote the newest unanswered reply, not the author", func(t *testing.T) {
		older := commentSpec{by: "kim", at: t2, url: "b"}
		newer := commentSpec{by: "sam", at: t3, url: "c"}
		pr := newPR(7, "lee").withThreads(thread(false, mine, older), thread(false, mine, newer))

		got := buildTasks(me, nil, nil, []pullRequest{pr})

		assertEqual(t, "sam", got[0].Person)
		assertEqual(t, "https://avatars.test/sam", got[0].AvatarURL)
	})

	t.Run("a deleted author gives no person", func(t *testing.T) {
		pr := newPR(7, "sam").requestedFrom(user(me), t1)
		pr.Author = nil
		reply := newPR(8, "lee").withThreads(thread(false, mine, commentSpec{by: "sam", at: t2, url: "b"}))
		reply.ReviewThreads.Nodes[0].LastComment.Nodes[0].Author = nil
		reply.ReviewThreads.Nodes[0].Comments.Nodes[1].Author = nil

		got := buildTasks(me, nil, []pullRequest{pr}, []pullRequest{reply})

		assertEqual(t, "02", itoa(len(got)))
		for _, tk := range got {
			assertEqual(t, "", tk.Person)
			assertEqual(t, "", tk.AvatarURL)
		}
	})

	t.Run("a bot gives no person", func(t *testing.T) {
		byType := newPR(7, "dependabot").requestedFrom(user(me), t1)
		byType.Author.Typename = "Bot"
		byLogin := newPR(8, "renovate[bot]").requestedFrom(user(me), t1)
		botReply := newPR(9, "lee").withThreads(thread(false, mine, commentSpec{by: "ci[bot]", at: t2, url: "b"}))

		got := buildTasks(me, nil, []pullRequest{byType, byLogin}, []pullRequest{botReply})

		assertEqual(t, "03", itoa(len(got)))
		for _, tk := range got {
			assertEqual(t, "", tk.Person)
			assertEqual(t, "", tk.AvatarURL)
		}
	})
}

func TestSameTasks_NoticesAChangedPerson(t *testing.T) {
	base := task.Task{Key: "k", Person: "sam", AvatarURL: "https://avatars.test/sam"}
	photo := base
	photo.AvatarURL = "https://avatars.test/sam2"
	name := base
	name.Person = "kim"

	assertEqual(t, "true", strconv.FormatBool(sameTasks([]task.Task{base}, []task.Task{base})))
	assertEqual(t, "false", strconv.FormatBool(sameTasks([]task.Task{base}, []task.Task{photo})))
	assertEqual(t, "false", strconv.FormatBool(sameTasks([]task.Task{base}, []task.Task{name})))
}

func TestActor_DecodesTheFieldsTheQueriesAsk(t *testing.T) {
	var a actor
	if err := json.Unmarshal([]byte(`{"__typename":"Bot","login":"x","avatarUrl":"https://avatars.test/x"}`), &a); err != nil {
		t.Fatal(err)
	}

	want := actor{Typename: "Bot", Login: "x", AvatarURL: "https://avatars.test/x"}
	if want != a {
		t.Fatalf("want %+v, got %+v", want, a)
	}
}
