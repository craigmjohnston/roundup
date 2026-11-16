# Source protocol

A source is a program that gives the menu bar app a list of tasks. The app starts it, the program writes what it knows to its standard output, and the app writes requests to its standard input. You can write one in any language. This file is the whole reference.

Add the program to the app's configuration file (see the README):

```json
{"sources": [{"command": ["/path/to/program", "arg"]}]}
```

## Transport

- Both directions carry newline-delimited JSON: one JSON object per line, UTF-8, ended by `\n`. A line can be as long as you need.
- Every object has a `type` string. Field names are camelCase.
- Times are RFC 3339 strings, such as `"2026-11-16T09:30:00Z"`. Leave a time out when it has no value.
- Leave out any field whose value is empty: `""`, `false`, `0`, `null` and `[]` all mean the same as leaving it out.
- Unknown fields are ignored, so a program can add its own.
- Write only protocol lines to standard output, and flush after each line. Write logs to standard error: the app copies each line to its own log, prefixed with the source `id`.

## Lifecycle

1. The app starts `command` and waits up to 10 seconds for the first line, which must be `hello`. It needs the hello to know the source's name, icons and sections before it draws anything. If there is no valid hello in time, the app logs why, stops the program and runs without the source until the app is restarted.
2. The program sends `tasks` whenever its list changes.
3. The app sends `refresh` when the panel opens and when the Mac wakes, as a request to check for changes now.
4. When the program exits, the app logs it and starts it again 30 seconds later. The new run starts with a new `hello`; the app keeps using the description from the first one. Until the new run sends `tasks`, the app keeps showing the last list.
5. When the app quits, the program's standard input closes. Exit when it does.

A line that is not valid JSON, or a `tasks` line whose tasks do not decode, is logged and skipped. A message with an unknown `type` is ignored without a log line, so later protocol versions can add messages.

## Messages from the program

### `hello`

Must be the first line.

```json
{"type":"hello","protocol":1,"source":{
  "id":"example",
  "name":"Example",
  "icon":"mark",
  "icons":{"mark":{"svg":"<svg ...>","template":true}},
  "sections":[{"name":"Releases","order":1,"fixed":true}],
  "canSignIn":false
}}
```

| Field | Meaning |
| --- | --- |
| `protocol` | Always `1`. The app rejects the program otherwise. |
| `source.id` | Required. Stable and unique across sources. It keys the person's dismissals, so do not change it. |
| `source.name` | How people see the source, as in "Example is signed out". Defaults to `id`. |
| `source.icon` | The key in `icons` of the source's mark: drawn on the grey tile that leads a row with no person or status, and on the corner of a status or photo lead unless a task names a `badge`. |
| `source.icons` | Named icons. Each is `{"svg": "<svg markup as a string>", "template": bool}`. A `template` icon is drawn in the text colour, so give it a single colour; any other icon is drawn in its own colours. |
| `source.sections` | The headings this source puts tasks under (see Sections). |
| `source.canSignIn` | `true` when the program handles `sign_in`. The app then shows a "Sign in" button while the source is signed out. |

### `tasks`

The complete current set of tasks, every time it changes. The app replaces the source's previous list with it, so send `[]` when nothing is open.

```json
{"type":"tasks","tasks":[{"key":"pr-12","title":"Fix the login redirect"}]}
```

A task has these fields. Only `key` is required.

| Field | Meaning |
| --- | --- |
| `key` | Unique within the source and stable across updates. Dismissals and row identity depend on it. |
| `title` | The row's first line. Empty shows a muted "Loading…", for a title you are still fetching. |
| `url` | Opened when the row is clicked. |
| `due` | When the task is due. Sorts the list and shows as "due 3 Feb" on a row with no readouts. |
| `created` | When the task appeared. Sorts the list and the sections. Changing it makes a dismissed task show again. |
| `person` | The display name of whoever the task is from. Empty when it is from no one. |
| `avatarUrl` | Where the person's profile photo can be downloaded. |
| `section` | Lists the task under that heading below the main list and leaves it out of the count. |
| `lead` | `{"symbol": "<SF Symbol name>", "tone": "<tone>"}`. Leads the row with a status circle. |
| `badge` | A key in the source's `icons`: the mark on the lead's corner instead of the source icon. |
| `detail` | The left of the row's second line. |
| `readouts` | The right of the second line, in order (see below). |
| `outcome` | `"failed"` or `"succeeded"`. Counts the task in the menu bar's red or green number. |

`source` is not needed: the app sets it from `source.id`.

A readout has these fields:

| Field | Meaning |
| --- | --- |
| `symbol` | An optional SF Symbol name, shown before the text. |
| `tone` | The colour of the symbol. |
| `text` | The readout's text. |
| `alert` | `true` colours the text with `tone` as well, and lets it clip with an ellipsis when space is short. |
| `since` | A time. When set, the app draws a live time from it instead of `text`. |
| `clock` | With `since`: `"elapsed"` counts up from `since` as `m:ss`, or `h:mm:ss` from an hour. `"ago"` shows how long ago `since` was: `now`, `4m ago`, `3h ago`, `2d ago`. |

A tone is one of `neutral` (the default), `accent`, `success`, `warning`, `danger`.

### `signed_out`

The program cannot read its data until the person signs in. The app drops the source's tasks and shows "<name> is signed out", with a "Sign in" button when `canSignIn` is true. Send it instead of `tasks`. The app asks for nothing more from the source until the person presses "Sign in", so send `signed_out` only from a program with `canSignIn`.

### `signed_in`

The answer to a `sign_in` that worked. Send `tasks` after it.

### `sign_in_failed`

The answer to a `sign_in` that did not work.

```json
{"type":"sign_in_failed","error":"The password was wrong"}
```

The app shows `error` under the signed-out notice, so write it for the person.

## Messages from the app

### `refresh`

```json
{"type":"refresh"}
```

Check for changes now. A program that is already pushing live updates can ignore it.

### `sign_in`

```json
{"type":"sign_in"}
```

Sent only when `canSignIn` is true, after the person presses "Sign in". Do whatever signing in takes, which may open a browser, then answer with `signed_in` or `sign_in_failed`. The button shows "Signing in…" until you answer.

## How rows are drawn

A row has a 28-point round lead on the left, two lines of text in the middle, and a dismiss button that appears on hover.

- **Lead.** With `lead`, a circle tinted by its `tone` around its SF Symbol. Without it, the person's photo (`avatarUrl`) when `person` is set, otherwise the source's icon on a grey tile. A circle or photo gets a small mark on its lower-right corner: the task's `badge` icon, or else the source's own icon. The icon is the source's `icon` key, or `badge` key, looked up in `icons`.
- **Title.** The first line, on one line, clipped with an ellipsis.
- **Second line.** `detail` on the left in muted text. On the right, the `readouts` in order, with a gap between them. A readout with a `symbol` draws it in its `tone` (muted when neutral) before the text; without one it is muted text. A readout with `alert` is coloured by its tone and keeps its room before `detail` does. A task with no readouts shows its `due` date there instead.
- **Click.** Opens `url`, then closes the panel.
- **Dismiss.** Hides the task as it is: the app remembers `key` with `created`, and shows the task again if `created` changes. Dismissals of tasks that leave the list are forgotten.
- **Symbols.** Any SF Symbol name works. The app draws a few common ones at tuned sizes (`checkmark`, `xmark`, `plusminus`, `bubble.left.fill`) and the rest at a default size.

### Order and count

- The main list is the tasks with no `section`. It is sorted by `due`, soonest first, with tasks that have no due date after them in order of `created`; the person can switch it to newest `created` first.
- The number on the panel header and in the menu bar counts the main list only. The menu bar also shows the number of tasks with outcome `failed` (red) and `succeeded` (green), counted across the main list and the sections.

### Sections

A task with a `section` is listed under that heading below the main list, newest `created` first. The heading's layout comes from the matching entry in `source.sections`:

- `order`: lower sorts first. Sections with equal `order` sort by name. A section a task names without declaring it has order 0.
- `fixed`: a fixed section shows in full and never scrolls, and goes below the scrolling sections. Others share the panel's height with the main list and scroll.

Sections from different sources with the same `name` are one section; the definition from the source with the lowest `id` wins.

### The empty and signed-out states

- Until a source has sent `tasks` or `signed_out`, its tasks are not shown and the panel shows a loading state.
- A source that is signed out shows a notice above the list: its icon, "<name> is signed out", and a "Sign in" button when `canSignIn` is true.

## Example session

`>` is a line the program writes, `<` a line the app writes.

```
> {"type":"hello","protocol":1,"source":{"id":"builds","name":"Builds","icon":"mark","icons":{"mark":{"svg":"<svg viewBox=\"0 0 16 16\"><circle cx=\"8\" cy=\"8\" r=\"6\"/></svg>","template":true}},"sections":[{"name":"Builds","order":1}],"canSignIn":true}}
> {"type":"signed_out"}
< {"type":"sign_in"}
> {"type":"signed_in"}
> {"type":"tasks","tasks":[{"key":"b-41","title":"Build 41 on main","url":"https://ci.example/b/41","created":"2026-11-16T09:30:00Z","section":"Builds","lead":{"symbol":"xmark","tone":"danger"},"detail":"main","outcome":"failed","readouts":[{"symbol":"xmark","tone":"danger","text":"tests failed","alert":true},{"since":"2026-11-16T09:41:00Z","clock":"ago"}]},{"key":"b-42","title":"Build 42 on main","url":"https://ci.example/b/42","created":"2026-11-16T09:50:00Z","section":"Builds","lead":{"symbol":"ellipsis","tone":"accent"},"detail":"main","readouts":[{"since":"2026-11-16T09:50:00Z","clock":"elapsed"}]}]}
< {"type":"refresh"}
> {"type":"tasks","tasks":[]}
```

In this session the program starts signed out and the person presses "Sign in". After the sign-in it lists a failed build and a running one under a "Builds" heading, and then, after a refresh, no builds at all.

## Writing a source in Go

The package `github.com/craigmjohnston/roundup/protocol` does the transport for a Go program: the hello, the `tasks` lines, the sign-in messages, `refresh` and `sign_in` from the app, and a clean exit when standard input closes.

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	info := task.Info{ID: "example", Name: "Example"}
	run := func(ctx context.Context, update func([]task.Task)) error {
		update([]task.Task{{Key: "1", Title: "First"}})
		<-ctx.Done()
		return nil
	}
	if err := protocol.Serve(ctx, os.Stdin, os.Stdout, info, run, protocol.Hooks{}); err != nil {
		log.Fatal(err)
	}
}
```

- `Serve(ctx, in, out, info, run, hooks)` sends the hello from `info` and calls `run`, which sends a snapshot through `update` each time the list changes and returns when `ctx` ends. It returns nil when `in` closes or `ctx` ends, and otherwise the error `run` returned.
- `Hooks.Refresh`, when set, is called for each `refresh`. `Hooks.SignIn`, when set, makes the hello say `canSignIn` and is called for each `sign_in`; its result becomes `signed_in` or `sign_in_failed`.
- When `run` returns `task.ErrSignedOut`, `Serve` sends `signed_out`, waits for a `sign_in`, and calls `run` again after `Hooks.SignIn` succeeds. A program with no `Hooks.SignIn` exits with that error instead.
- `ServeSource(ctx, in, out, src)` takes a `task.Source` and wires up `Refresh` and `SignIn` when `src` implements `task.Refresher` and `task.SignIn`.
- `protocol/protocoltest` runs a program built on `Serve` over in-memory pipes in a test, with the app's side of the conversation.

Write logs to standard error, as the transport rules say; nothing else may write to standard output.

### Making a Go source installable

The app can install a Go source straight from a repository: a `package` entry in the configuration names `<import path>@<version>`, and the app runs `go install` for it. For that to work:

- The package is a `main` package, such as `example.com/acme/tool/cmd/acme-source`. The binary is named after the last element of the import path.
- Its module has no `replace` directives, because `go install pkg@version` refuses a module that has them. A module of its own for the plugins, apart from any app code that needs a `replace`, keeps them out of the way.
- Every module it requires is reachable at a tagged or pseudo-version. A private repository needs `GOPRIVATE` set and git credentials in the app's environment.

A version tag such as `v1.2.0` pins the install; `latest` installs again at each start of the app.

## Checking a program by hand

Run the program in a terminal, type a `refresh` line and read what it prints. Each line it prints must be one complete JSON object.
