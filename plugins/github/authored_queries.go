package github

import (
	"strings"
	"time"
)

const (
	authoredOpenSearch = "is:open is:pr author:@me archived:false"
	detailBatch        = 10
)

// authoredListQuery reads every open pull request of the viewer, with the head commit's check counts, which change without updatedAt.
const authoredListQuery = `query($open: String!) {
  viewer { login }
  rateLimit { cost remaining resetAt }
  open: search(query: $open, type: ISSUE, first: 50) {
    nodes { ... on PullRequest {
      id number title url isDraft createdAt updatedAt reviewDecision
      repository { nameWithOwner }
      commits(last: 1) { nodes { commit { statusCheckRollup {
        state
        contexts { totalCount checkRunCountsByState { state count } statusContextCountsByState { state count } }
      } } } }
    } }
  }
}`

// authoredDetailQuery reads the conversation of a batch of pull requests, for the pull requests whose updatedAt changed.
const authoredDetailQuery = `query($ids: [ID!]!) {
  rateLimit { cost remaining resetAt }
  nodes(ids: $ids) { ... on PullRequest {
    id
    comments(last: 30) { nodes { createdAt author { __typename login } reactionGroups { viewerHasReacted } } }
    reviews(last: 20) { nodes { state body createdAt author { __typename login } reactionGroups { viewerHasReacted } } }
    reviewThreads(last: 50) { nodes {
      isResolved
      comments(last: 1) { nodes { author { __typename login } reactionGroups { viewerHasReacted } } }
    } }
  } }
}`

func authoredOpenQuery(now time.Time) string {
	return authoredOpenSearch + " updated:>=" + now.Add(-staleAfter).UTC().Format("2006-01-02")
}

type authoredListData struct {
	Viewer struct {
		Login string `json:"login"`
	} `json:"viewer"`
	Open struct {
		Nodes []authoredPR `json:"nodes"`
	} `json:"open"`
}

type authoredDetailData struct {
	Nodes []*prConversation `json:"nodes"`
}

func ownerAndName(nameWithOwner string) (owner, name string) {
	owner, name, _ = strings.Cut(nameWithOwner, "/")
	return owner, name
}

type repoRef struct {
	NameWithOwner    string `json:"nameWithOwner"`
	DefaultBranchRef *struct {
		Name string `json:"name"`
	} `json:"defaultBranchRef"`
}

type authoredPR struct {
	ID             string    `json:"id"`
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	IsDraft        bool      `json:"isDraft"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	ReviewDecision string    `json:"reviewDecision"`
	Repository     repoRef   `json:"repository"`
	Commits        struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *checkRollup `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type checkRollup struct {
	State    string `json:"state"`
	Contexts struct {
		TotalCount                 int          `json:"totalCount"`
		CheckRunCountsByState      []stateCount `json:"checkRunCountsByState"`
		StatusContextCountsByState []stateCount `json:"statusContextCountsByState"`
	} `json:"contexts"`
}

type stateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// account is a comment author; Typename is "Bot" for apps.
type account struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

type reactionGroup struct {
	ViewerHasReacted bool `json:"viewerHasReacted"`
}

// prConversation is what the unanswered-comments rule reads.
type prConversation struct {
	ID       string `json:"id"`
	Comments struct {
		Nodes []conversationItem `json:"nodes"`
	} `json:"comments"`
	Reviews struct {
		Nodes []conversationItem `json:"nodes"`
	} `json:"reviews"`
	ReviewThreads struct {
		Nodes []conversationThread `json:"nodes"`
	} `json:"reviewThreads"`
}

type conversationItem struct {
	State          string          `json:"state"`
	Body           string          `json:"body"`
	CreatedAt      time.Time       `json:"createdAt"`
	Author         *account        `json:"author"`
	ReactionGroups []reactionGroup `json:"reactionGroups"`
}

type conversationThread struct {
	IsResolved bool `json:"isResolved"`
	Comments   struct {
		Nodes []conversationItem `json:"nodes"`
	} `json:"comments"`
}
