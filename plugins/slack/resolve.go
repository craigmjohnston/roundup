package slack

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"strings"

	"github.com/craigmjohnston/roundup/task"
)

const (
	untitledMessage = "(message unavailable)"
	untitledSaved   = "Reminder"
)

type message struct {
	Ts         string                `json:"ts"`
	Text       string                `json:"text"`
	User       string                `json:"user"`
	Username   string                `json:"username"`
	Subtype    string                `json:"subtype"`
	BotID      string                `json:"bot_id"`
	BotProfile struct{ Name string } `json:"bot_profile"`
	Files      []struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	} `json:"files"`
	Attachments []struct {
		Fallback string `json:"fallback"`
		Title    string `json:"title"`
		Text     string `json:"text"`
	} `json:"attachments"`
}

func fatal(ctx context.Context, err error) bool {
	return errors.Is(err, task.ErrSignedOut) || ctx.Err() != nil
}

// humanAuthor is the id of the person who wrote the message, empty when a bot or an app did.
func (m message) humanAuthor() string {
	if m.BotID != "" || m.BotProfile.Name != "" || m.Subtype == "bot_message" {
		return ""
	}
	return m.User
}

func messageText(m message) string {
	if m.Text != "" {
		return m.Text
	}
	for _, a := range m.Attachments {
		if t := firstNonEmpty(a.Fallback, a.Title, a.Text); t != "" {
			return t
		}
	}
	for _, f := range m.Files {
		if name := firstNonEmpty(f.Title, f.Name); name != "" {
			return "File: " + name
		}
	}
	return ""
}

var slackToken = regexp.MustCompile(`<([^<>]+)>`)

// cleanText turns Slack's message markup into one line of plain text.
func cleanText(text string, userName func(id string) string) string {
	text = slackToken.ReplaceAllStringFunc(text, func(tok string) string {
		inner := tok[1 : len(tok)-1]
		target, label, hasLabel := strings.Cut(inner, "|")
		switch {
		case strings.HasPrefix(target, "@"):
			return "@" + userName(target[1:])
		case strings.HasPrefix(target, "#"):
			if hasLabel {
				return "#" + label
			}
			return "#" + target[1:]
		case strings.HasPrefix(target, "!"):
			if hasLabel {
				return label
			}
			return "@" + strings.SplitN(target[1:], "^", 2)[0]
		case hasLabel && label != "":
			return label
		default:
			return target
		}
	})
	return cleanSpace(html.UnescapeString(text))
}

func cleanSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// richTextOf flattens the rich_text blocks Slack uses for reminder text.
func richTextOf(raw json.RawMessage, userName func(id string) string) string {
	var node any
	if len(raw) == 0 || json.Unmarshal(raw, &node) != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case []any:
			for _, c := range v {
				walk(c)
			}
		case map[string]any:
			switch v["type"] {
			case "text":
				s, _ := v["text"].(string)
				b.WriteString(s)
			case "link":
				s, _ := v["text"].(string)
				if s == "" {
					s, _ = v["url"].(string)
				}
				b.WriteString(s)
			case "user":
				id, _ := v["user_id"].(string)
				b.WriteString("@" + userName(id))
			case "emoji":
				name, _ := v["name"].(string)
				b.WriteString(":" + name + ":")
			default:
				walk(v["elements"])
			}
		}
	}
	walk(node)
	return b.String()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
