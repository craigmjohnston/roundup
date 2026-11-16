// Package task is the contract between the task list and the sources that feed it.
package task

import (
	"context"
	"errors"
	"time"
)

type Task struct {
	// Key is unique within its source and stable across updates.
	Key string `json:"key"`
	// Source is the ID of the source's Info.
	Source string `json:"source"`
	// Title is empty until the source has loaded the task's text.
	Title   string    `json:"title,omitempty"`
	URL     string    `json:"url,omitempty"`
	Due     time.Time `json:"due,omitzero"`
	Created time.Time `json:"created,omitzero"`
	// Person is the display name of whoever the task is from; empty when it is from no one, such as a reminder or a bot.
	Person string `json:"person,omitempty"`
	// AvatarURL is where Person's profile photo can be downloaded; empty when there is none.
	AvatarURL string `json:"avatarUrl,omitempty"`
	// Section, when set, lists the task under that heading below the main list and leaves it out of the count.
	Section string `json:"section,omitempty"`
	// Lead, when set, leads the row with a status circle instead of the person's photo or the source tile.
	Lead *Status `json:"lead,omitempty"`
	// Badge names an icon from the source's Info.Icons to show on the lead's corner instead of the source icon.
	Badge string `json:"badge,omitempty"`
	// Detail is the left of the row's second line.
	Detail string `json:"detail,omitempty"`
	// Readouts sit at the right of the second line, in order. A task with none shows its due date there.
	Readouts []Readout `json:"readouts,omitempty"`
	// Outcome counts the task in the menu bar's failed or succeeded number.
	Outcome Outcome `json:"outcome,omitempty"`
}

// Tone is the colour family of a status or readout.
type Tone string

const (
	ToneNeutral Tone = "neutral"
	ToneAccent  Tone = "accent"
	ToneSuccess Tone = "success"
	ToneWarning Tone = "warning"
	ToneDanger  Tone = "danger"
)

// Clock is how a readout's Since is drawn.
type Clock string

const (
	// ClockElapsed counts up from Since as m:ss, or h:mm:ss from an hour.
	ClockElapsed Clock = "elapsed"
	// ClockAgo shows how long ago Since was, such as "4m ago".
	ClockAgo Clock = "ago"
)

// Outcome says which menu bar number a task counts towards.
type Outcome string

const (
	OutcomeFailed    Outcome = "failed"
	OutcomeSucceeded Outcome = "succeeded"
)

// Status is a row's lead: a circle tinted by Tone around an SF Symbol.
type Status struct {
	// Symbol is an SF Symbol name.
	Symbol string `json:"symbol"`
	Tone   Tone   `json:"tone,omitempty"`
}

// Readout is one item at the right of a row's second line.
type Readout struct {
	// Symbol is an optional SF Symbol name, coloured by Tone, or muted when Tone is neutral.
	Symbol string `json:"symbol,omitempty"`
	Tone   Tone   `json:"tone,omitempty"`
	Text   string `json:"text,omitempty"`
	// Alert colours Text with Tone too, and lets it clip with an ellipsis when space is short.
	Alert bool `json:"alert,omitempty"`
	// Since makes the app draw a live time instead of Text, as Clock says.
	Since time.Time `json:"since,omitzero"`
	Clock Clock     `json:"clock,omitempty"`
}

// Info describes a source to the app.
type Info struct {
	// ID is stable; it is Task.Source and keys the source's dismissals.
	ID string
	// Name is shown to people, as in "Slack is signed out".
	Name string
	// Icon is the key in Icons of the source's mark.
	Icon  string
	Icons map[string]Icon
	// Sections are the headings the source puts tasks under.
	Sections []Section
}

// Icon is an SVG a source supplies.
type Icon struct {
	SVG []byte
	// Template tints the icon with the text colour; otherwise it is drawn in its own colours.
	Template bool
}

// Section orders and lays out a heading.
type Section struct {
	Name string `json:"name"`
	// Order sorts lower first; sections with equal order sort by name.
	Order int `json:"order,omitempty"`
	// Fixed sections show in full and never scroll; they go below the scrolling ones.
	Fixed bool `json:"fixed,omitempty"`
}

// ErrSignedOut is what Run returns when the source needs the person to sign in.
var ErrSignedOut = errors.New("signed out")

// Source reports its complete current set of open tasks, as a snapshot, every time that set changes.
type Source interface {
	Info() Info
	// Run blocks until ctx ends. It returns ErrSignedOut when the source needs the person to sign in.
	Run(ctx context.Context, update func([]Task)) error
}

// SignIn is implemented by sources that can sign in; the app offers a "Sign in" button while they are signed out.
type SignIn interface {
	SignIn(ctx context.Context) error
}

// Refresher is implemented by sources that poll, to poll now. Sources that push live updates do not implement it.
type Refresher interface {
	Refresh()
}
