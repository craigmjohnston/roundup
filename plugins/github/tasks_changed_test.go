package github

import (
	"testing"
	"time"

	"github.com/craigmjohnston/roundup/task"
)

func TestSameTasks_NoticesChangesInsideTheGenericFields(t *testing.T) {
	at := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	build := func(edit func(*task.Task)) []task.Task {
		tk := task.Task{
			Key:    "k",
			Badge:  "staging",
			Detail: "widgets#1",
			Lead:   &task.Status{Symbol: "ellipsis", Tone: task.ToneWarning},
			Readouts: []task.Readout{
				{Since: at, Clock: task.ClockElapsed},
				{Symbol: "circle.fill", Tone: task.ToneWarning, Text: "1/2 jobs"},
			},
		}
		edit(&tk)
		return []task.Task{tk}
	}
	same := func(*task.Task) {}
	t.Run("ignores an identical task, however its times are zoned", func(t *testing.T) {
		later := build(func(tk *task.Task) { tk.Readouts[0].Since = at.In(time.FixedZone("x", 3600)) })
		assertEqual(t, true, sameTasks(build(same), later))
	})
	for name, edit := range map[string]func(*task.Task){
		"detail":         func(tk *task.Task) { tk.Detail = "other#1" },
		"badge":          func(tk *task.Task) { tk.Badge = "production" },
		"outcome":        func(tk *task.Task) { tk.Outcome = task.OutcomeFailed },
		"lead symbol":    func(tk *task.Task) { tk.Lead.Symbol = "xmark" },
		"lead tone":      func(tk *task.Task) { tk.Lead.Tone = task.ToneDanger },
		"lead appearing": func(tk *task.Task) { tk.Lead = nil },
		"readout symbol": func(tk *task.Task) { tk.Readouts[1].Symbol = "xmark" },
		"readout tone":   func(tk *task.Task) { tk.Readouts[1].Tone = task.ToneDanger },
		"readout text":   func(tk *task.Task) { tk.Readouts[1].Text = "2/2 jobs" },
		"readout alert":  func(tk *task.Task) { tk.Readouts[1].Alert = true },
		"readout clock":  func(tk *task.Task) { tk.Readouts[0].Clock = task.ClockAgo },
		"readout since":  func(tk *task.Task) { tk.Readouts[0].Since = at.Add(time.Second) },
		"readout count":  func(tk *task.Task) { tk.Readouts = tk.Readouts[:1] },
		"section":        func(tk *task.Task) { tk.Section = "b" },
	} {
		t.Run("notices a change of "+name, func(t *testing.T) {
			assertEqual(t, false, sameTasks(build(same), build(edit)))
		})
	}
	t.Run("notices a change of URL alone", func(t *testing.T) {
		a, b := build(same), build(same)
		b[0].URL = "https://app.circleci.com/pipelines/github/acme/widgets/1/workflows/w"
		assertEqual(t, false, sameTasks(a, b))
	})
	t.Run("notices a task appearing or disappearing", func(t *testing.T) {
		assertEqual(t, false, sameTasks([]task.Task{{Key: "k"}}, build(same)))
		assertEqual(t, false, sameTasks(build(same), []task.Task{{Key: "k"}}))
	})
}
