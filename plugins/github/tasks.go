package github

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

const staleAfter = 14 * 24 * time.Hour

// fresh drops pull requests with no activity for two weeks, which the list treats as stale.
func fresh(prs []pullRequest, now time.Time) []pullRequest {
	cutoff := now.Add(-staleAfter)
	return slices.DeleteFunc(slices.Clone(prs), func(pr pullRequest) bool { return pr.UpdatedAt.Before(cutoff) })
}

// buildTasks turns the two searches' pull requests into tasks, one per pull request.
// A personal review request takes precedence over an unanswered reply.
func buildTasks(viewer string, bots []string, requested, reviewed []pullRequest) []task.Task {
	var tasks []task.Task
	seen := map[string]bool{}
	for _, pr := range requested {
		if t, ok := requestTask(viewer, bots, pr); ok && !seen[t.Key] {
			seen[t.Key] = true
			tasks = append(tasks, t)
		}
	}
	for _, pr := range reviewed {
		if t, ok := replyTask(viewer, bots, pr); ok && !seen[t.Key] {
			seen[t.Key] = true
			tasks = append(tasks, t)
		}
	}
	slices.SortFunc(tasks, func(a, b task.Task) int { return strings.Compare(a.Key, b.Key) })
	return tasks
}

// requestTask is the task for a pull request that has a review requested from the viewer personally.
func requestTask(viewer string, bots []string, pr pullRequest) (task.Task, bool) {
	if pr.URL == "" || isViewer(viewer, pr.Author) {
		return task.Task{}, false
	}
	requested := false
	for _, n := range pr.ReviewRequests.Nodes {
		requested = requested || isPerson(viewer, n.RequestedReviewer)
	}
	if !requested {
		return task.Task{}, false
	}
	var created time.Time
	for _, ev := range pr.TimelineItems.Nodes {
		if isPerson(viewer, ev.RequestedReviewer) && ev.CreatedAt.After(created) {
			created = ev.CreatedAt
		}
	}
	if created.IsZero() {
		created = pr.UpdatedAt
	}
	t := task.Task{
		Key:     key(pr),
		Source:  reviewsSource,
		Title:   pr.Title,
		Detail:  detail(pr),
		URL:     pr.URL,
		Created: created,
	}
	t.Person, t.AvatarURL = whoFrom(pr.Author, bots)
	return t, true
}

// replyTask is the task for a pull request where someone replied to the viewer's review comment and the viewer has not answered.
func replyTask(viewer string, bots []string, pr pullRequest) (task.Task, bool) {
	if pr.URL == "" || isViewer(viewer, pr.Author) {
		return task.Task{}, false
	}
	if pr.ViewerLatestReview != nil && pr.ViewerLatestReview.State == "APPROVED" {
		return task.Task{}, false
	}
	var newest *comment
	for _, th := range pr.ReviewThreads.Nodes {
		if th.IsResolved || len(th.LastComment.Nodes) == 0 {
			continue
		}
		last := th.LastComment.Nodes[0]
		if isViewer(viewer, last.Author) || !viewerCommented(viewer, th) {
			continue
		}
		if newest == nil || last.CreatedAt.After(newest.CreatedAt) {
			newest = &last
		}
	}
	if newest == nil {
		return task.Task{}, false
	}
	t := task.Task{
		Key:     key(pr),
		Source:  reviewsSource,
		Title:   pr.Title,
		Detail:  detail(pr) + " · reply from " + login(newest.Author),
		URL:     newest.URL,
		Created: newest.CreatedAt,
	}
	t.Person, t.AvatarURL = whoFrom(newest.Author, bots)
	return t, true
}

func viewerCommented(viewer string, th reviewThread) bool {
	return slices.ContainsFunc(th.Comments.Nodes, func(c comment) bool { return isViewer(viewer, c.Author) })
}

func key(pr pullRequest) string {
	return fmt.Sprintf("%s#%d", pr.Repository.NameWithOwner, pr.Number)
}

func detail(pr pullRequest) string {
	_, repo, _ := strings.Cut(pr.Repository.NameWithOwner, "/")
	return fmt.Sprintf("%s#%d · %s", repo, pr.Number, login(pr.Author))
}

func isViewer(viewer string, a *actor) bool {
	return a != nil && strings.EqualFold(a.Login, viewer)
}

func isPerson(viewer string, r *reviewer) bool {
	return r != nil && r.Typename == "User" && strings.EqualFold(r.Login, viewer)
}

// login is the author's login, or "ghost" for a deleted account, which GitHub returns as null.
func login(a *actor) string {
	if a == nil {
		return "ghost"
	}
	return a.Login
}

// whoFrom is who a task is from: the actor's login and photo, or nothing for a deleted account or a bot.
func whoFrom(a *actor, bots []string) (name, avatarURL string) {
	if a == nil || automated(a.Login, a.Typename, bots) {
		return "", ""
	}
	return a.Login, a.AvatarURL
}

func sameTasks(a, b []task.Task) bool {
	return slices.EqualFunc(a, b, func(x, y task.Task) bool {
		return x.Key == y.Key && x.Source == y.Source && x.Title == y.Title && x.Detail == y.Detail &&
			x.URL == y.URL && x.Person == y.Person && x.AvatarURL == y.AvatarURL && x.Section == y.Section &&
			x.Due.Equal(y.Due) && x.Created.Equal(y.Created) && x.Badge == y.Badge && x.Outcome == y.Outcome &&
			sameLead(x.Lead, y.Lead) && slices.EqualFunc(x.Readouts, y.Readouts, sameReadout)
	})
}

func sameLead(a, b *task.Status) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameReadout(a, b task.Readout) bool {
	return a.Symbol == b.Symbol && a.Tone == b.Tone && a.Text == b.Text && a.Alert == b.Alert && a.Clock == b.Clock && a.Since.Equal(b.Since)
}
