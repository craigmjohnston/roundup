package github

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

// Authored is the task source for the user's own open pull requests.
type Authored struct {
	conn
	// BotLogins are accounts that act for automation but appear as ordinary users.
	BotLogins     []string
	conversations map[string]cachedConversation
}

type cachedConversation struct {
	updatedAt  time.Time
	unanswered int
}

var (
	_ task.Source    = (*Authored)(nil)
	_ task.Refresher = (*Authored)(nil)
)

func NewAuthored() *Authored {
	return &Authored{conn: newConn()}
}

func (a *Authored) Info() task.Info {
	return githubInfo(authoredSource, task.Section{Name: authoredSection})
}

// Run polls until ctx ends, then returns nil.
// It keeps the last snapshot when a poll fails and returns an error when gh has no token or GitHub rejects it.
func (a *Authored) Run(ctx context.Context, update func([]task.Task)) error {
	return a.run(ctx, a.poll, update)
}

func (a *Authored) poll(ctx context.Context, token string) ([]task.Task, error) {
	now := a.now()
	var list authoredListData
	vars := map[string]any{"open": authoredOpenQuery(now)}
	if err := a.graphQL(ctx, token, authoredListQuery, vars, &list); err != nil {
		return nil, fmt.Errorf("listing pull requests: %w", err)
	}
	viewer := list.Viewer.Login

	open := slices.DeleteFunc(list.Open.Nodes, func(pr authoredPR) bool { return pr.UpdatedAt.Before(now.Add(-staleAfter)) })
	if err := a.loadConversations(ctx, token, viewer, open); err != nil {
		return nil, fmt.Errorf("loading conversations: %w", err)
	}
	var tasks []task.Task
	for _, pr := range open {
		tasks = append(tasks, openTask(pr, a.conversations[pr.ID].unanswered))
	}
	slices.SortFunc(tasks, func(x, y task.Task) int { return strings.Compare(x.Key, y.Key) })
	return tasks, nil
}

// loadConversations counts unanswered comments for pull requests that are new or updated since cached, and evicts the ones no longer listed.
func (a *Authored) loadConversations(ctx context.Context, token, viewer string, prs []authoredPR) error {
	if a.conversations == nil {
		a.conversations = map[string]cachedConversation{}
	}
	var stale []authoredPR
	for _, pr := range prs {
		if c, ok := a.conversations[pr.ID]; !ok || !c.updatedAt.Equal(pr.UpdatedAt) {
			stale = append(stale, pr)
		}
	}
	for batch := range slices.Chunk(stale, detailBatch) {
		updated := map[string]time.Time{}
		ids := make([]string, len(batch))
		for i, pr := range batch {
			ids[i] = pr.ID
			updated[pr.ID] = pr.UpdatedAt
		}
		var data authoredDetailData
		if err := a.graphQL(ctx, token, authoredDetailQuery, map[string]any{"ids": ids}, &data); err != nil {
			return err
		}
		for _, n := range data.Nodes {
			if n != nil {
				a.conversations[n.ID] = cachedConversation{updatedAt: updated[n.ID], unanswered: unansweredCount(viewer, a.BotLogins, *n)}
			}
		}
	}
	listed := map[string]bool{}
	for _, pr := range prs {
		listed[pr.ID] = true
	}
	maps.DeleteFunc(a.conversations, func(id string, _ cachedConversation) bool { return !listed[id] })
	return nil
}
