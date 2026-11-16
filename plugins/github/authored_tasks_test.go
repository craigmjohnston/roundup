package github

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func openPR(number int) authoredPR {
	pr := authoredPR{ID: "AP_" + itoa(number), Number: number, Title: "Add the widget", URL: "https://github.com/acme/widgets/pull/" + itoa(number), CreatedAt: t1, UpdatedAt: t3}
	pr.Repository.NameWithOwner = "acme/widgets"
	return pr
}

func (pr authoredPR) withChecks(state string) authoredPR {
	return pr.withRollup(&checkRollup{State: state})
}

func (pr authoredPR) withRollup(r *checkRollup) authoredPR {
	pr.Commits.Nodes = make([]struct {
		Commit struct {
			StatusCheckRollup *checkRollup `json:"statusCheckRollup"`
		} `json:"commit"`
	}, 1)
	pr.Commits.Nodes[0].Commit.StatusCheckRollup = r
	return pr
}

func assertEqual[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Errorf("want %v, got %v", want, got)
	}
}

func person(login string) *account { return &account{Typename: "User", Login: login} }

func item(author *account, at time.Time) conversationItem {
	return conversationItem{Author: author, CreatedAt: at}
}

func (c conversationItem) reacted() conversationItem {
	c.ReactionGroups = []reactionGroup{{ViewerHasReacted: false}, {ViewerHasReacted: true}}
	return c
}

func (c conversationItem) review(state, body string) conversationItem {
	c.State, c.Body = state, body
	return c
}

func convo(comments []conversationItem, reviews []conversationItem, threads ...conversationThread) prConversation {
	var c prConversation
	c.Comments.Nodes = comments
	c.Reviews.Nodes = reviews
	c.ReviewThreads.Nodes = threads
	return c
}

func lastComment(resolved bool, last conversationItem) conversationThread {
	th := conversationThread{IsResolved: resolved}
	th.Comments.Nodes = []conversationItem{last}
	return th
}

func TestUnansweredCount_Threads(t *testing.T) {
	t.Run("counts an unresolved thread whose last comment is from a person", func(t *testing.T) {
		assertEqual(t, 1, unansweredCount(me, nil, convo(nil, nil, lastComment(false, item(person("sam"), t1)))))
	})
	t.Run("counts each unanswered thread once", func(t *testing.T) {
		c := convo(nil, nil, lastComment(false, item(person("sam"), t1)), lastComment(false, item(person("alex"), t2)))
		assertEqual(t, 2, unansweredCount(me, nil, c))
	})
	t.Run("ignores a resolved thread", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, lastComment(true, item(person("sam"), t1)))))
	})
	t.Run("ignores a thread the user replied to last", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, lastComment(false, item(person(me), t2)))))
	})
	t.Run("ignores a thread whose last comment the user reacted to", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, lastComment(false, item(person("sam"), t1).reacted()))))
	})
	t.Run("ignores a bot by type", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, lastComment(false, item(&account{Typename: "Bot", Login: "copilot"}, t1)))))
	})
	t.Run("ignores a bot by login suffix", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, lastComment(false, item(&account{Typename: "User", Login: "dependabot[bot]"}, t1)))))
	})
	t.Run("ignores an empty thread", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, conversationThread{})))
	})
	t.Run("matches the user's login without case", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo(nil, nil, lastComment(false, item(person("Jane"), t1)))))
	})
}

func TestUnansweredCount_Conversation(t *testing.T) {
	t.Run("counts a comment with no later reply from the user", func(t *testing.T) {
		assertEqual(t, 1, unansweredCount(me, nil, convo([]conversationItem{item(person("sam"), t1)}, nil)))
	})
	t.Run("counts each unanswered comment", func(t *testing.T) {
		c := convo([]conversationItem{item(person("sam"), t1), item(person("alex"), t2)}, nil)
		assertEqual(t, 2, unansweredCount(me, nil, c))
	})
	t.Run("ignores comments before a later reply from the user", func(t *testing.T) {
		c := convo([]conversationItem{item(person("sam"), t1), item(person("alex"), t1.Add(time.Hour)), item(person(me), t2)}, nil)
		assertEqual(t, 0, unansweredCount(me, nil, c))
	})
	t.Run("counts a comment made after the user's last reply", func(t *testing.T) {
		c := convo([]conversationItem{item(person("sam"), t1), item(person(me), t2), item(person("alex"), t3)}, nil)
		assertEqual(t, 1, unansweredCount(me, nil, c))
	})
	t.Run("ignores a comment the user reacted to", func(t *testing.T) {
		assertEqual(t, 0, unansweredCount(me, nil, convo([]conversationItem{item(person("sam"), t1).reacted()}, nil)))
	})
	t.Run("ignores a bot comment", func(t *testing.T) {
		c := convo([]conversationItem{item(&account{Typename: "Bot", Login: "codecov"}, t1), item(&account{Login: "netlify[bot]"}, t2)}, nil)
		assertEqual(t, 0, unansweredCount(me, nil, c))
	})
	t.Run("counts a review summary with a body", func(t *testing.T) {
		c := convo(nil, []conversationItem{item(person("sam"), t1).review("COMMENTED", "Looks good overall")})
		assertEqual(t, 1, unansweredCount(me, nil, c))
	})
	t.Run("ignores a review summary without a body", func(t *testing.T) {
		c := convo(nil, []conversationItem{item(person("sam"), t1).review("APPROVED", "  ")})
		assertEqual(t, 0, unansweredCount(me, nil, c))
	})
	t.Run("ignores a pending review", func(t *testing.T) {
		c := convo(nil, []conversationItem{item(person("sam"), t1).review("PENDING", "draft")})
		assertEqual(t, 0, unansweredCount(me, nil, c))
	})
	t.Run("treats a later review summary from the user as a reply", func(t *testing.T) {
		c := convo([]conversationItem{item(person("sam"), t1)}, []conversationItem{item(person(me), t2).review("COMMENTED", "Done")})
		assertEqual(t, 0, unansweredCount(me, nil, c))
	})
	t.Run("does not treat a review summary from the user without a body as a reply", func(t *testing.T) {
		c := convo([]conversationItem{item(person("sam"), t1)}, []conversationItem{item(person(me), t2).review("APPROVED", "")})
		assertEqual(t, 1, unansweredCount(me, nil, c))
	})
	t.Run("adds threads and conversation comments", func(t *testing.T) {
		c := convo([]conversationItem{item(person("sam"), t1)}, nil, lastComment(false, item(person("alex"), t1)))
		assertEqual(t, 2, unansweredCount(me, nil, c))
	})
}

func rollup(runs map[string]int, statuses map[string]int) *checkRollup {
	r := &checkRollup{}
	for _, state := range slices.Sorted(maps.Keys(runs)) {
		r.Contexts.CheckRunCountsByState = append(r.Contexts.CheckRunCountsByState, stateCount{state, runs[state]})
		r.Contexts.TotalCount += runs[state]
	}
	for _, state := range slices.Sorted(maps.Keys(statuses)) {
		r.Contexts.StatusContextCountsByState = append(r.Contexts.StatusContextCountsByState, stateCount{state, statuses[state]})
		r.Contexts.TotalCount += statuses[state]
	}
	return r
}

func TestCheckCounts(t *testing.T) {
	counts := func(r *checkRollup) pullRequestState {
		state, passed, total := checkCounts(r)
		return pullRequestState{ChecksState: state, ChecksPassed: passed, ChecksTotal: total}
	}
	want := func(state string, passed, total int) pullRequestState {
		return pullRequestState{ChecksState: state, ChecksPassed: passed, ChecksTotal: total}
	}

	t.Run("is empty when the pull request has no checks", func(t *testing.T) {
		assertEqual(t, want("", 0, 0), counts(nil))
		assertEqual(t, want("", 0, 0), counts(&checkRollup{State: "SUCCESS"}))
	})
	t.Run("passes when every check and status passed", func(t *testing.T) {
		assertEqual(t, want(checksPassed, 12, 12), counts(rollup(map[string]int{"SUCCESS": 9}, map[string]int{"SUCCESS": 3})))
	})
	t.Run("fails when any check failed, even while others run", func(t *testing.T) {
		assertEqual(t, want(checksFailed, 9, 12), counts(rollup(map[string]int{"SUCCESS": 8, "FAILURE": 1, "IN_PROGRESS": 1}, map[string]int{"SUCCESS": 1, "PENDING": 1})))
	})
	t.Run("fails on an errored status", func(t *testing.T) {
		assertEqual(t, want(checksFailed, 4, 5), counts(rollup(nil, map[string]int{"SUCCESS": 4, "ERROR": 1})))
	})
	t.Run("runs while any check is pending, queued, in progress, waiting or expected", func(t *testing.T) {
		for _, state := range []string{"PENDING", "QUEUED", "IN_PROGRESS", "WAITING"} {
			assertEqual(t, want(checksRunning, 3, 4), counts(rollup(map[string]int{"SUCCESS": 3, state: 1}, nil)))
		}
		assertEqual(t, want(checksRunning, 3, 4), counts(rollup(nil, map[string]int{"SUCCESS": 3, "EXPECTED": 1})))
	})
	t.Run("counts neutral and skipped checks as passed", func(t *testing.T) {
		assertEqual(t, want(checksPassed, 4, 4), counts(rollup(map[string]int{"SUCCESS": 2, "NEUTRAL": 1, "SKIPPED": 1}, nil)))
	})
	t.Run("counts cancelled, timed out and other unsuccessful checks as failed", func(t *testing.T) {
		for _, state := range []string{"CANCELLED", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED", "STALE"} {
			assertEqual(t, want(checksFailed, 3, 4), counts(rollup(map[string]int{"SUCCESS": 3, state: 1}, nil)))
		}
	})
}

func TestPRStatus(t *testing.T) {
	type result struct{ status, reason string }
	status := func(decision string, comments int, checks string) result {
		s, r := prStatus(decision, comments, checks)
		return result{s, r}
	}
	t.Run("needs attention for failed checks before anything else", func(t *testing.T) {
		assertEqual(t, result{prAttention, reasonChecks}, status("CHANGES_REQUESTED", 2, checksFailed))
		assertEqual(t, result{prAttention, reasonChecks}, status("APPROVED", 0, checksFailed))
	})
	t.Run("needs attention for requested changes before a comment to answer", func(t *testing.T) {
		assertEqual(t, result{prAttention, reasonChanges}, status("CHANGES_REQUESTED", 2, checksPassed))
	})
	t.Run("needs attention for a comment to answer before approval", func(t *testing.T) {
		assertEqual(t, result{prAttention, reasonComment}, status("APPROVED", 1, checksRunning))
	})
	t.Run("is approved when nothing needs attention", func(t *testing.T) {
		assertEqual(t, result{prApproved, ""}, status("APPROVED", 0, checksPassed))
		assertEqual(t, result{prApproved, ""}, status("APPROVED", 0, checksRunning))
	})
	t.Run("is waiting otherwise", func(t *testing.T) {
		assertEqual(t, result{prWaiting, ""}, status("REVIEW_REQUIRED", 0, checksPassed))
		assertEqual(t, result{prWaiting, ""}, status("", 0, ""))
		assertEqual(t, result{prWaiting, ""}, status("REVIEW_REQUIRED", 0, checksRunning))
	})
}

func TestPullRequestStatus(t *testing.T) {
	t.Run("fills the reference, checks, comments and status", func(t *testing.T) {
		pr := openPR(12).withRollup(rollup(map[string]int{"SUCCESS": 11, "FAILURE": 1}, nil))
		pr.ReviewDecision = "APPROVED"
		want := pullRequestState{Ref: "widgets#12", Status: prAttention, Reason: reasonChecks, ChecksState: checksFailed, ChecksPassed: 11, ChecksTotal: 12, Comments: 2}
		assertEqual(t, want, pullRequestStatus(pr, 2))
	})
	t.Run("treats a draft by the same rules", func(t *testing.T) {
		pr := openPR(12)
		pr.IsDraft = true
		pr.ReviewDecision = "APPROVED"
		assertEqual(t, prApproved, pullRequestStatus(pr, 0).Status)
		pr.ReviewDecision = "CHANGES_REQUESTED"
		assertEqual(t, prAttention, pullRequestStatus(pr, 0).Status)
		assertEqual(t, prAttention, pullRequestStatus(openPR(12).withChecks("SUCCESS"), 1).Status)
		pr.ReviewDecision = ""
		assertEqual(t, prWaiting, pullRequestStatus(pr, 0).Status)
	})
	t.Run("has no checks when the head commit has no rollup", func(t *testing.T) {
		pr := pullRequestStatus(openPR(12), 0)
		assertEqual(t, "", pr.ChecksState)
		assertEqual(t, 0, pr.ChecksTotal)
	})
}

func TestTaskFields(t *testing.T) {
	open := openTask(openPR(12), 0)
	assertEqual(t, "acme/widgets#12", open.Key)
	assertEqual(t, "github-authored", open.Source)
	assertEqual(t, "Pull requests", open.Section)
	assertEqual(t, "Add the widget", open.Title)
	assertEqual(t, "https://github.com/acme/widgets/pull/12", open.URL)
	assertEqual(t, true, open.Due.IsZero())
	assertEqual(t, true, open.Created.Equal(t1))
	assertEqual(t, "widgets#12", open.Detail)
	assertEqual(t, "", open.Badge)
}

func TestPullRequestLead(t *testing.T) {
	for name, tc := range map[string]struct {
		state pullRequestState
		want  task.Status
	}{
		"waiting":                         {pullRequestState{Status: prWaiting}, task.Status{Symbol: "ellipsis", Tone: task.ToneNeutral}},
		"attention for changes":           {pullRequestState{Status: prAttention, Reason: reasonChanges}, task.Status{Symbol: "plusminus", Tone: task.ToneAccent}},
		"attention for a comment":         {pullRequestState{Status: prAttention, Reason: reasonComment}, task.Status{Symbol: "bubble.left.fill", Tone: task.ToneAccent}},
		"attention for checks":            {pullRequestState{Status: prAttention, Reason: reasonChecks}, task.Status{Symbol: "xmark", Tone: task.ToneDanger}},
		"attention for an unknown reason": {pullRequestState{Status: prAttention, Reason: "other"}, task.Status{Symbol: "exclamationmark", Tone: task.ToneAccent}},
		"approved":                        {pullRequestState{Status: prApproved}, task.Status{Symbol: "checkmark", Tone: task.ToneSuccess}},
	} {
		t.Run(name, func(t *testing.T) {
			assertEqual(t, tc.want, *tc.state.lead())
		})
	}
}

func TestPullRequestReadouts(t *testing.T) {
	t.Run("lists the comments, then the checks", func(t *testing.T) {
		got := pullRequestState{Comments: 8, ChecksState: checksFailed, ChecksPassed: 11, ChecksTotal: 12}.readouts()
		want := []task.Readout{
			{Symbol: "bubble.left.fill", Tone: task.ToneNeutral, Text: "8"},
			{Symbol: "xmark", Tone: task.ToneDanger, Text: "11/12"},
		}
		if !slices.Equal(want, got) {
			t.Errorf("want %+v, got %+v", want, got)
		}
	})
	t.Run("tones the checks by state", func(t *testing.T) {
		for state, want := range map[string]task.Readout{
			checksPassed:  {Symbol: "checkmark", Tone: task.ToneSuccess, Text: "3/4"},
			checksFailed:  {Symbol: "xmark", Tone: task.ToneDanger, Text: "3/4"},
			checksRunning: {Symbol: "circle.fill", Tone: task.ToneWarning, Text: "3/4"},
		} {
			got := pullRequestState{ChecksState: state, ChecksPassed: 3, ChecksTotal: 4}.readouts()
			if len(got) != 1 || got[0] != want {
				t.Errorf("%s: want %+v, got %+v", state, want, got)
			}
		}
	})
	t.Run("has none without comments or checks", func(t *testing.T) {
		assertEqual(t, 0, len(pullRequestState{}.readouts()))
	})
}

func TestOpenTaskOutcome(t *testing.T) {
	failing := openPR(12).withRollup(rollup(map[string]int{"SUCCESS": 1, "FAILURE": 1}, nil))
	assertEqual(t, task.OutcomeFailed, openTask(failing, 0).Outcome)
	assertEqual(t, task.Outcome(""), openTask(openPR(13).withChecks("SUCCESS"), 0).Outcome)
}

func TestAuthoredInfo(t *testing.T) {
	info := NewAuthored().Info()
	assertEqual(t, "github-authored", info.ID)
	assertEqual(t, "GitHub", info.Name)
	assertEqual(t, true, info.Icons[info.Icon].Template)
	assertEqual(t, 1, len(info.Sections))
	assertEqual(t, task.Section{Name: "Pull requests"}, info.Sections[0])
}

func TestAuthoredTasks_HaveNoPerson(t *testing.T) {
	open := openTask(openPR(12), 2)
	assertEqual(t, "", open.Person)
	assertEqual(t, "", open.AvatarURL)
}

func TestAutomated(t *testing.T) {
	tests := []struct {
		name, login, typename string
		want                  bool
	}{
		{name: "a person", login: "sam", typename: "User", want: false},
		{name: "an app", login: "codecov", typename: "Bot", want: true},
		{name: "a bot login", login: "dependabot[bot]", typename: "User", want: true},
		{name: "a machine user", login: "deploybot", typename: "User", want: true},
		{name: "a machine user in another case", login: "DeployBot", typename: "User", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := automated(tc.login, tc.typename, []string{"deploybot"}); got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}
