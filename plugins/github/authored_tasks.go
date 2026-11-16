package github

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/craigmjohnston/roundup/task"
)

const (
	authoredSource  = "github-authored"
	authoredSection = "Pull requests"
)

// Pull request statuses. Attention means the author has something to act on.
const (
	prWaiting   = "waiting"
	prAttention = "attention"
	prApproved  = "approved"
)

// Reasons a pull request needs attention, in the order they take precedence.
const (
	reasonChecks  = "checks"
	reasonChanges = "changes"
	reasonComment = "comment"
)

// States of a set of checks.
const (
	checksPassed  = "passed"
	checksFailed  = "failed"
	checksRunning = "running"
)

// States of a check run or a status context, by how they count towards a pull request's checks.
var (
	passingStates = []string{"SUCCESS", "NEUTRAL", "SKIPPED", "COMPLETED"}
	runningStates = []string{"PENDING", "QUEUED", "IN_PROGRESS", "WAITING", "EXPECTED"}
)

// openTask is the task for one of the viewer's open pull requests, with its unanswered comment count.
func openTask(pr authoredPR, unanswered int) task.Task {
	st := pullRequestStatus(pr, unanswered)
	t := task.Task{
		Key:      authoredKey(pr.Repository, pr.Number),
		Source:   authoredSource,
		Title:    pr.Title,
		Detail:   st.Ref,
		URL:      pr.URL,
		Created:  pr.CreatedAt,
		Section:  authoredSection,
		Lead:     st.lead(),
		Readouts: st.readouts(),
	}
	if st.ChecksState == checksFailed {
		t.Outcome = task.OutcomeFailed
	}
	return t
}

// pullRequestState is what a pull request row shows: reference, checks, comments and the status they add up to.
type pullRequestState struct {
	// Ref is the short reference, such as "widgets#14667".
	Ref    string
	Status string
	// Reason says why Status is prAttention; empty otherwise.
	Reason string
	// ChecksState is empty when the pull request has no checks.
	ChecksState  string
	ChecksPassed int
	ChecksTotal  int
	// Comments counts the comments from people that the author has not answered.
	Comments int
}

func pullRequestStatus(pr authoredPR, unanswered int) pullRequestState {
	checks, passed, total := checkCounts(headRollup(pr))
	status, reason := prStatus(pr.ReviewDecision, unanswered, checks)
	return pullRequestState{
		Ref:          repoNumber(pr.Repository, pr.Number),
		Status:       status,
		Reason:       reason,
		ChecksState:  checks,
		ChecksPassed: passed,
		ChecksTotal:  total,
		Comments:     unanswered,
	}
}

func (p pullRequestState) lead() *task.Status {
	switch p.Status {
	case prAttention:
		switch p.Reason {
		case reasonChecks:
			return &task.Status{Symbol: "xmark", Tone: task.ToneDanger}
		case reasonChanges:
			return &task.Status{Symbol: "plusminus", Tone: task.ToneAccent}
		case reasonComment:
			return &task.Status{Symbol: "bubble.left.fill", Tone: task.ToneAccent}
		}
		return &task.Status{Symbol: "exclamationmark", Tone: task.ToneAccent}
	case prApproved:
		return &task.Status{Symbol: "checkmark", Tone: task.ToneSuccess}
	}
	return &task.Status{Symbol: "ellipsis", Tone: task.ToneNeutral}
}

// readouts are the unanswered comment count, when there is one, then the checks.
func (p pullRequestState) readouts() []task.Readout {
	var out []task.Readout
	if p.Comments > 0 {
		out = append(out, task.Readout{Symbol: "bubble.left.fill", Tone: task.ToneNeutral, Text: strconv.Itoa(p.Comments)})
	}
	checks := task.Readout{Text: fmt.Sprintf("%d/%d", p.ChecksPassed, p.ChecksTotal)}
	switch p.ChecksState {
	case checksPassed:
		checks.Symbol, checks.Tone = "checkmark", task.ToneSuccess
	case checksFailed:
		checks.Symbol, checks.Tone = "xmark", task.ToneDanger
	case checksRunning:
		checks.Symbol, checks.Tone = "circle.fill", task.ToneWarning
	default:
		return out
	}
	return append(out, checks)
}

// prStatus applies the attention rules in order: failed checks, then changes requested, then a comment to answer.
func prStatus(reviewDecision string, comments int, checksState string) (status, reason string) {
	switch {
	case checksState == checksFailed:
		return prAttention, reasonChecks
	case reviewDecision == "CHANGES_REQUESTED":
		return prAttention, reasonChanges
	case comments > 0:
		return prAttention, reasonComment
	case reviewDecision == "APPROVED":
		return prApproved, ""
	}
	return prWaiting, ""
}

func headRollup(pr authoredPR) *checkRollup {
	if len(pr.Commits.Nodes) == 0 {
		return nil
	}
	return pr.Commits.Nodes[0].Commit.StatusCheckRollup
}

// checkCounts reads a check rollup the way GitHub's summary does.
// Neutral and skipped checks count as passed; cancelled, timed out and any other unlisted state count as failed.
func checkCounts(r *checkRollup) (state string, passed, total int) {
	if r == nil || r.Contexts.TotalCount == 0 {
		return "", 0, 0
	}
	failed, running := 0, 0
	for _, c := range slices.Concat(r.Contexts.CheckRunCountsByState, r.Contexts.StatusContextCountsByState) {
		switch {
		case slices.Contains(passingStates, c.State):
			passed += c.Count
		case slices.Contains(runningStates, c.State):
			running += c.Count
		default:
			failed += c.Count
		}
	}
	switch {
	case failed > 0:
		return checksFailed, passed, r.Contexts.TotalCount
	case running > 0:
		return checksRunning, passed, r.Contexts.TotalCount
	}
	return checksPassed, passed, r.Contexts.TotalCount
}

func authoredKey(repo repoRef, number int) string {
	return fmt.Sprintf("%s#%d", repo.NameWithOwner, number)
}

func repoNumber(repo repoRef, number int) string {
	_, name := ownerAndName(repo.NameWithOwner)
	return fmt.Sprintf("%s#%d", name, number)
}

// unansweredCount counts the review threads and conversation comments from people that the viewer has not answered.
func unansweredCount(viewer string, bots []string, c prConversation) int {
	count := 0
	for _, th := range c.ReviewThreads.Nodes {
		if th.IsResolved || len(th.Comments.Nodes) == 0 {
			continue
		}
		if last := th.Comments.Nodes[0]; unansweredBy(viewer, bots, last) {
			count++
		}
	}

	var convo []conversationItem
	convo = append(convo, c.Comments.Nodes...)
	for _, r := range c.Reviews.Nodes {
		if r.State != "PENDING" && strings.TrimSpace(r.Body) != "" {
			convo = append(convo, r)
		}
	}
	slices.SortStableFunc(convo, func(a, b conversationItem) int { return a.CreatedAt.Compare(b.CreatedAt) })
	for i, item := range convo {
		if !unansweredBy(viewer, bots, item) {
			continue
		}
		later := slices.ContainsFunc(convo[i+1:], func(l conversationItem) bool { return isViewer(viewer, toActor(l.Author)) })
		if !later {
			count++
		}
	}
	return count
}

// unansweredBy reports whether the item is from a person other than the viewer, who has not reacted to it.
func unansweredBy(viewer string, bots []string, item conversationItem) bool {
	if isBot(item.Author, bots) || isViewer(viewer, toActor(item.Author)) {
		return false
	}
	return !slices.ContainsFunc(item.ReactionGroups, func(g reactionGroup) bool { return g.ViewerHasReacted })
}

func isBot(a *account, bots []string) bool {
	return a != nil && automated(a.Login, a.Typename, bots)
}

// automated reports whether an account is an app, a bot, or one of the configured bot logins, which GitHub reports as ordinary users.
func automated(login, typename string, bots []string) bool {
	return typename == "Bot" || strings.HasSuffix(login, "[bot]") || slices.ContainsFunc(bots, func(m string) bool { return strings.EqualFold(m, login) })
}

func toActor(a *account) *actor {
	if a == nil {
		return nil
	}
	return &actor{Login: a.Login}
}
