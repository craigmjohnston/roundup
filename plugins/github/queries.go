package github

import "time"

const (
	requestedSearch = "is:open is:pr user-review-requested:@me archived:false"
	repliesSearch   = "is:open is:pr reviewed-by:@me -author:@me archived:false"
	threadsBatch    = 5
)

// withCutoff limits a search to pull requests updated within the stale window.
func withCutoff(search string, now time.Time) string {
	return search + " updated:>=" + now.Add(-staleAfter).UTC().Format("2006-01-02")
}

// requestedQuery pages 50 pull requests, with up to 30 requested reviewers and 30 review-request events each.
const requestedQuery = `query($q: String!, $after: String) {
  viewer { login }
  rateLimit { cost remaining resetAt }
  search(query: $q, type: ISSUE, first: 50, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest {
      number title url updatedAt
      repository { nameWithOwner }
      author { __typename login avatarUrl(size: 72) }
      reviewRequests(first: 30) { nodes { requestedReviewer { __typename ... on User { login } } } }
      timelineItems(last: 30, itemTypes: [REVIEW_REQUESTED_EVENT]) {
        nodes { ... on ReviewRequestedEvent { createdAt requestedReviewer { __typename ... on User { login } } } }
      }
    } }
  }
}`

// repliesQuery pages 25 pull requests without their review threads, which threadsQuery fetches for the ones that changed.
const repliesQuery = `query($q: String!, $after: String) {
  viewer { login }
  rateLimit { cost remaining resetAt }
  search(query: $q, type: ISSUE, first: 25, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest {
      id number title url updatedAt
      repository { nameWithOwner }
      author { __typename login avatarUrl(size: 72) }
      viewerLatestReview { state }
    } }
  }
}`

// threadsQuery loads up to 50 review threads, with up to 50 comments each, for a batch of pull requests.
const threadsQuery = `query($ids: [ID!]!) {
  rateLimit { cost remaining resetAt }
  nodes(ids: $ids) { ... on PullRequest {
    id
    reviewThreads(first: 50) { nodes {
      isResolved
      comments(first: 50) { nodes { author { __typename login avatarUrl(size: 72) } } }
      lastComment: comments(last: 1) { nodes { url createdAt author { __typename login avatarUrl(size: 72) } } }
    } }
  } }
}`

type searchData struct {
	Viewer struct {
		Login string `json:"login"`
	} `json:"viewer"`
	RateLimit struct {
		Cost      int       `json:"cost"`
		Remaining int       `json:"remaining"`
		ResetAt   time.Time `json:"resetAt"`
	} `json:"rateLimit"`
	Search struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []pullRequest `json:"nodes"`
	} `json:"search"`
}

type threadsData struct {
	Nodes []*struct {
		ID            string `json:"id"`
		ReviewThreads struct {
			Nodes []reviewThread `json:"nodes"`
		} `json:"reviewThreads"`
	} `json:"nodes"`
}

type actor struct {
	Typename  string `json:"__typename"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
}

type reviewer struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

type pullRequest struct {
	ID         string    `json:"id"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Author         *actor `json:"author"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *reviewer `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	TimelineItems struct {
		Nodes []struct {
			CreatedAt         time.Time `json:"createdAt"`
			RequestedReviewer *reviewer `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"timelineItems"`
	ViewerLatestReview *struct {
		State string `json:"state"`
	} `json:"viewerLatestReview"`
	ReviewThreads struct {
		Nodes []reviewThread `json:"nodes"`
	} `json:"reviewThreads"`
}

type reviewThread struct {
	IsResolved bool `json:"isResolved"`
	Comments   struct {
		Nodes []comment `json:"nodes"`
	} `json:"comments"`
	LastComment struct {
		Nodes []comment `json:"nodes"`
	} `json:"lastComment"`
}

type comment struct {
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	Author    *actor    `json:"author"`
}
