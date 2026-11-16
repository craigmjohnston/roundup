package slack

import (
	"testing"

	"github.com/craigmjohnston/roundup/task"
)

func byKey(tasks []task.Task) map[string]task.Task {
	out := map[string]task.Task{}
	for _, tk := range tasks {
		out[tk.Key] = tk
	}
	return out
}

func TestLater_AHumanAuthorGivesThePersonAndTheirPhoto(t *testing.T) {
	tests := []struct {
		name    string
		profile map[string]any
		want    string
	}{
		{name: "the 72 pixel photo", profile: map[string]any{"image_72": "https://img.test/72", "image_48": "https://img.test/48", "image_192": "https://img.test/192"}, want: "https://img.test/72"},
		{name: "the 48 pixel photo when there is no 72", profile: map[string]any{"image_48": "https://img.test/48", "image_192": "https://img.test/192"}, want: "https://img.test/48"},
		{name: "the 192 pixel photo when there is neither", profile: map[string]any{"image_192": "https://img.test/192"}, want: "https://img.test/192"},
		{name: "no photo", profile: nil, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSlack(t)
			f.channels["C1"] = map[string]any{"name": "devs"}
			f.users["U1"] = "Jane Doe"
			f.profiles["U1"] = tc.profile
			f.history["C1/1.1"] = msg("1.1", "U1", "hello")
			f.setItems(item("C1", "1.1", 0, 1))

			got := runSource(t, f.later()).settle(t)

			assertEqual(t, "Jane Doe", got[0].Person)
			assertEqual(t, tc.want, got[0].AvatarURL)
			assertEqual(t, "#devs · Jane Doe", got[0].Detail)
		})
	}
}

func TestLater_BotsAppsAndRemindersHaveNoPerson(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.users["U1"] = "Jane"
	f.users["B1"] = "Review Bot"
	f.userAs["B1"] = map[string]any{"is_bot": true}
	f.profiles["B1"] = map[string]any{"image_72": "https://img.test/bot"}
	f.users["U3"] = "Some App"
	f.history["C1/1.1"] = msg("1.1", "B1", "from a bot user")
	f.history["C1/1.2"] = map[string]any{"ts": "1.2", "subtype": "bot_message", "username": "Deploys", "text": "bot message"}
	app := msg("1.3", "U3", "from an app")
	app["bot_id"] = "B9"
	f.history["C1/1.3"] = app
	profiled := msg("1.4", "U3", "with a bot profile")
	profiled["bot_profile"] = map[string]any{"name": "Some App"}
	f.history["C1/1.4"] = profiled
	reminder := item("Sa1", "", 0, 5)
	reminder["item_type"] = "reminder"
	f.setItems(item("C1", "1.1", 0, 1), item("C1", "1.2", 0, 2), item("C1", "1.3", 0, 3), item("C1", "1.4", 0, 4), reminder)

	got := runSource(t, f.later()).settle(t)

	assertEqual(t, "5", itoa(len(got)))
	for _, tk := range got {
		assertEqual(t, "", tk.Person)
		assertEqual(t, "", tk.AvatarURL)
	}
	assertEqual(t, "#devs · Review Bot", byKey(got)["C1/1.1"].Detail)
}

func TestLater_APersonIsFilledInOnceTheAuthorIsLookedUp(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	f.profiles["U1"] = map[string]any{"image_72": "https://img.test/72"}
	release := f.holdMessages()
	r := runSource(t, f.later())

	before := r.next(t)
	release()
	after := r.settle(t)

	for _, tk := range before {
		assertEqual(t, "", tk.Person)
		assertEqual(t, "", tk.AvatarURL)
	}
	for _, tk := range after {
		assertEqual(t, "Jane", tk.Person)
		assertEqual(t, "https://img.test/72", tk.AvatarURL)
	}
}

func TestLater_AWarmStartKeepsThePersonWithoutLookups(t *testing.T) {
	f := newFakeSlack(t)
	seedTwoItems(f)
	f.profiles["U1"] = map[string]any{"image_72": "https://img.test/72"}
	first := runSource(t, f.later())
	first.settle(t)
	first.stop(t)
	before := lookupCalls(f)

	second := runSource(t, f.later())
	warm := second.next(t)

	assertEqual(t, "0", itoa(lookupCalls(f)-before))
	for _, tk := range warm {
		assertEqual(t, "Jane", tk.Person)
		assertEqual(t, "https://img.test/72", tk.AvatarURL)
	}
}

func TestLater_AWarmStartStillHidesABotUser(t *testing.T) {
	f := newFakeSlack(t)
	f.channels["C1"] = map[string]any{"name": "devs"}
	f.users["B1"] = "Review Bot"
	f.userAs["B1"] = map[string]any{"is_bot": true}
	f.history["C1/1.1"] = msg("1.1", "B1", "hi")
	f.setItems(item("C1", "1.1", 0, 1))
	first := runSource(t, f.later())
	first.settle(t)
	first.stop(t)

	warm := runSource(t, f.later()).next(t)

	assertEqual(t, "", warm[0].Person)
}
