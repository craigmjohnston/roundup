# Roundup: how it works

A macOS menu bar app, built on MyGo, that lists what is waiting on the person in one panel. `README.md` covers building and configuring it; `PROTOCOL.md` is the reference for external sources. This file is for someone working on the code.

## Layout of the code

| Path | Holds |
|---|---|
| `task/` | The contract between sources and the app: `Task`, `Info`, `Source`, and the optional `SignIn` and `Refresher` interfaces. |
| `cmd/roundup/` | The app: state, panel layout, rows, menu bar readout, dismissals, avatars. It names no service; everything it draws comes from `task`. |
| `external/` | A `task.Source` backed by a child program that speaks `PROTOCOL.md`. |
| `protocol/` | The message shapes of `PROTOCOL.md`, shared by `external/` and by plugins, and `Serve`, the Go entry point that turns a source into a program that speaks the protocol. `protocoltest/` drives such a program over pipes in tests. |
| `config/` | The configuration file: the list of sources, each a `command` or a Go `package`. |
| `install/` | Installs a `package` source with `go install` into the user config directory and returns the binary's path. |
| `plugins/slack/` | The Slack source and the client behind it. |
| `plugins/slack/cmd/roundup-slack/` | The Slack plugin program. |
| `plugins/slack/cmd/slk/` | A command-line client for the same Slack session, for trying out calls and events. |
| `plugins/github/` | The GitHub sources. |
| `plugins/github/cmd/roundup-github/` | The GitHub plugin program: one source per process, named by its argument. |

Nothing under `cmd/roundup/` imports `plugins/`; `TestAppDependsOnNoPlugin` fails if it does. A plugin is a separate program that the app runs as an entry in `sources`.

## Modules

There are two Go modules, tied together for local work by `go.work` at the repository root.

| Module | Path | Holds |
|---|---|---|
| Root | `github.com/craigmjohnston/roundup` | The app, `task`, `protocol`, `config`, `external`, `install`. Depends on MyGo, through a `replace` to the patched copy in `third_party/mygo`. |
| Plugins | `github.com/craigmjohnston/roundup/plugins` | The Slack and GitHub plugins. Depends on the root module for `task` and `protocol`, and on chromedp, cobra and go-keyring. It has no `replace` and no MyGo dependency, so `go install` accepts it. |

`go install pkg@version` refuses a module whose `go.mod` has a `replace`, which is why the plugins are their own module. The plugins module requires the root module at the placeholder version `v0.0.0-00010101000000-000000000000`; `go.work` has a `replace` for that version that points at the repository root, so local builds use the working tree. `make build`, `make test` and `go vet` cover both modules with `./... ./plugins/...`. Run `go mod tidy` in each module with `GOWORK=off`.

## Task model

A source describes how its rows look, so the app needs no per-service code.

- `Task` has a `Key` (unique in its source, stable across updates), `Title` (empty shows "Loading…"), `URL`, `Due`, `Created`, `Person` and `AvatarURL`, and an optional `Section`.
- `Lead` is a `Status` (SF Symbol plus `Tone`) drawn as a status circle. Without it the row leads with the person's photo, or the source's icon on a grey tile when there is no person.
- `Badge` names an icon from the source's `Info.Icons` for the lead's corner, instead of the source's own icon.
- `Detail` is the left of the second line. `Readouts` sit at its right, in order; a task with none shows its due date there. A `Readout` is a symbol and tone, text, and optionally `Alert` (coloured text that clips with an ellipsis) or `Since` plus `Clock` (the app draws a live elapsed time or "4m ago").
- `Outcome` (`failed` or `succeeded`) counts the task in the menu bar's red or green number.
- A source's `Info` has an `ID` (the task's `Source`, and the key of its dismissals), a `Name`, the `Icons` it registers (SVG; a template icon is tinted with the text colour), and the `Sections` it uses.
- `Run(ctx, update)` blocks and calls `update` with the complete current set of tasks each time it changes. It returns `task.ErrSignedOut` when the person must sign in. A source that implements `SignIn` gets a "Sign in" button; one that implements `Refresher` is asked to poll when the panel opens and when the Mac wakes.

## Sources

Every source is a program listed under `sources` in the configuration, started by `external.Open`. A `package` source is first resolved to a binary by `install.Installer.Resolve`, which runs `go install` with `GOBIN` and `GOWORK=off` set. The app opens all of them at once, so startup takes as long as the slowest install and `hello` (a hello takes up to 10 seconds) and the panel lists sources in configuration order. It restarts a program 30 seconds after it exits and keeps the last list while it is down. A program that fails to start is logged and left out until the app restarts. A configuration with a `slack` or `github` key stops the app at startup.

The plugins in this repository are built with `protocol.ServeSource`:

| Program | ID | Section |
|---|---|---|
| `roundup-slack -workspace <name>` | `slack-later` | none: main list |
| `roundup-github reviews` | `github-reviews` | none: main list |
| `roundup-github authored` | `github-authored` | "Pull requests" |

The IDs key the person's dismissals, so they do not change. Slack signs in from the plugin when the app sends `sign_in`: Chrome opens and the session goes to the Keychain. A plugin's end-to-end tests run its source through `protocol.ServeSource` over `protocoltest` pipes, next to the source's own fakes.

The same thing can show up from two sources, such as a bot in Slack announcing a pull request that the GitHub source also lists. Nothing merges them.

## Panel

### List, sections and counts

- The main list is the tasks with no section, sorted by due date (soonest first, undated after, then by `Created`) or by newest `Created`. The header's sort menu switches between them.
- A task with a section is listed under that heading below the main list, newest first. Sections are ordered by `Info.Sections[].Order`, then name. A fixed section shows in full and goes below the scrolling ones. Same-named sections from different sources are one section; the lowest source ID defines it.
- The number in the header and in the menu bar counts the main list only.
- Any task can be dismissed from its hover button. A dismissal matches on source, key and `Created`, so a task whose `Created` changes shows again. Dismissals for tasks no longer in a snapshot are dropped.
- A source that returns `ErrSignedOut` shows a notice above the list with its name and a "Sign in" button when it can.
- With no sources the panel says where the configuration file goes; with sources but no tasks it says "No tasks".

### Rows

- A row is 28 points of lead, then a one-line title and a second line, with a dismiss button on hover. Times are on the second line so titles get the full width. Due dates read "due 12 Nov".
- The lead is a 28-point circle. A status circle is tinted with its tone at 16% (grey for neutral) around the symbol in the full tone colour. The corner badge is a 15-point ring with an 11-point icon, outset 3; a task's own badge icon is a 17-point ring with a 13-point icon, outset 4.
- Symbols are centred geometrically, with no optical nudge; a nudge to offset the badge looked off-centre at 1x. `cmd/roundup/symbols.go` has one table of per-symbol sizes and weights for leads and readouts, with a default for unknown symbols.
- In the second line, a row with an `Alert` readout keeps its `Detail` whole and clips the alert text instead. Readouts are 10 points apart.
- Icons are SF Symbols, drawn by macOS.

### Height

The panel is 380 points wide and as tall as its content, up to 640. Fixed sections always show in full. The main list and the scrolling sections share what is left: each gets what it needs when both fit; otherwise the smaller need is met up to half the space and the other takes the rest and scrolls. `cmd/roundup/layout.go` computes this from measured row sizes, and `TestLayoutMatchesTheRenderedPanel` checks those sizes against a rendered panel. The window is created at the computed height and resized when the content changes while it is open.

### Menu bar readout

The readout is a capsule drawn as an image and set as the menu bar item's non-template icon. It holds a checklist symbol with the open-task count (empty until the first source answers), then a red disc with a cross and the failed count, then a green disc with a tick and the succeeded count; each of those shows only when above zero. A spinner shows until a source has answered.

- The capsule is the item's height less 3 points above and below, kept within 22 to 26 points (24 when unknown). Digits are 0.57 of that height, discs 0.625.
- macOS draws its own highlight behind an item while it is pressed and while its panel shows. The readout is drawn twice, with and without the capsule, at the same size; the second is the button's alternate image, which AppKit shows while the item is highlighted, so only the system's highlight shows then.
- On a dark bar the numbers are white, the discs are `#ff8a80` and `#5ee68c` with dark glyphs, and the capsule is black at 17%. On a light bar the numbers are `#1c1e22`, the discs are `#c81e12` and `#0f7a36` with white glyphs, and the capsule is white at 33%.
- The image is drawn at the scale of the display the item is on, and its PNG carries a pHYs chunk so macOS sizes it in points. It is redrawn when the counts, the bar's light or dark appearance or the display change.
- Layout centres a symbol's box, not its ink, which leaves a disc's glyph up to a pixel off. `stampGlyph` finds the disc's pixels and draws the glyph with its ink centred on them; `TestReadoutGlyphsAreCentredOnTheirDiscs` checks every height, both scales and both bar colours.
- Judge the readout at 1x and at actual size as well as enlarged: smoothed or enlarged renders hide what goes wrong at 1x.

### MyGo patch

MyGo fixes every menu bar icon at 18 points tall with padding either side, and has no alternate image. `patches/mygo-v0.9.0-tray-icon.patch` changes `internal/darwin/menu.go` and `modules.go`: in `SetImage`, a template icon is still scaled to 18 points, while a coloured icon keeps its own size and the item's length is set to its width; `Tray.SetHighlightedIcon` sets the button's alternate image and adds contents to what its cell highlights by. The status item's cell highlights by nothing, since macOS 26 draws the highlight outside the button, so without that the alternate image never shows. `make mygo` copies v0.9.0 from the module cache into `third_party/mygo` (ignored by git) and applies the patch, and `go.mod` replaces MyGo with that copy. Moving to a newer MyGo means rebasing the patch.

## Slack

The Later source (`slack-later`) uses the same session Slack's web client does. There is no OAuth app: `Authenticate` opens Chrome on the workspace, waits for the `d` session cookie, reads the client token (`xoxc-…`) from the workspace page, and stores both in the login Keychain under the service `slk`. The Chrome profile is kept in `~/Library/Application Support/slk/chrome-profile/` and a 1Password link is made into it so the extension works. Slack answers 403 to workspace page requests with no browser user agent. `client.go` allows only an allowlist of read-only methods.

### What the source does

1. Opens the event connection: `rtm.connect`, then a websocket with the session cookie, pinged every 30 seconds. After a failure it reconnects with a backoff from 1 second to 1 minute. A session Slack rejects returns `ErrSignedOut`.
2. Loads every item from `saved.list` (50 per page, cursor paged) whose state is not `completed`. That includes to-dos and saved items.
3. Applies `saved_added`, `saved_updated` and `saved_deleted` events to the set as they arrive.
4. Resolves display text in steps, publishing after each: channels (`users.conversations`), message text (`messages.list`), then user names (`users.info`). A title shows "Loading…" until its text is known. Lookups that do not change are cached in `~/Library/Caches/roundup/slack-<workspace>.json`.

A task links to `slack://channel?team=<team>&id=<channel>&message=<ts>`, which opens the message in the desktop app. An item with no message, such as a reminder, links to `slack://open?team=<team>`. Slack's deep-link docs list only `open`, `channel`, `user`, `file`, `share-file` and `app`, and say unknown paths fall back to `slack://open`; no link opens the To-dos or Saved view.

The Later source has no age cut-off: every uncompleted item shows, so a long list shows there is a backlog.

### API behaviour

Private methods, as the web client calls them:

- `saved.list`: items with `item_id`, `item_type` (`message` or `reminder`), `ts`, `state`, `todo_state`, `date_created`, `date_due`, `date_completed`. Reminder items have no `ts`, and their text is in `description` as rich-text blocks. `todo_state` is `to_do` or `saved`; Slack's "Later is now To-dos" change turned Later items that had a reminder into to-dos and left the rest as saved items.
- `messages.list`: form field `message_ids`, a JSON array of `{"channel": "C…", "timestamps": ["…"]}`. It returns `messages` keyed by channel, in the shape of `conversations.history`, with thread replies included. At most 20 channels per call: 22 gave `too_many_channels`. The source falls back to `conversations.history` and then `conversations.replies` for a message the bulk call leaves out.
- `users.conversations` with `types=public_channel,private_channel,mpim,im` and `limit=1000` returns every conversation the user is in.
- `users.info` with `users=<comma-separated ids>` takes many at once, and may return fewer than asked; the source asks for the rest one at a time. `users.list` fails with `fatal_error` on this kind of session.
- `/remind` creates a Later item with no message behind it; its id is not a channel id.

Events on the websocket, each seen within about a second of the action:

| Action in Slack | Event | Carries |
|---|---|---|
| Read a channel or DM | `channel_marked`, `im_marked` | channel, new read marker, unread and mention counts |
| Mark a channel unread | `channel_marked` | marker moved back, unread count above zero |
| Save a message for later | `saved_added` | the item |
| Complete, un-complete or re-date an item | `saved_updated` | the item with its new state or due date |
| Remove an item | `saved_deleted` | the item |
| An item becomes due | `saved_due` | the item |
| Any badge change | `badge_counts_updated` | the same totals as `client.counts` |
| New message in a channel | `message` | the message |

The `saved_*` events carry the item under a `saved` key, in the shape of a `saved.list` item, plus `counts` and `event_ts`. The app applies only `saved_added`, `saved_updated` and `saved_deleted`. `slk listen` prints every event's type.

Not seen live, known from other clients' source:

- Thread read markers: `thread_marked` and `thread_subscribed` events carry `subscription {type, channel, thread_ts, date_create, active, last_read}`.
- Followed threads: the private method `subscriptions.thread.getView` (`limit`, `fetch_threads_state=true`, `priority_mode=all`) returns each thread's parent with `last_read` and `latest_reply`; a thread is unread when `latest_reply` is after `last_read`. A parent from `conversations.replies` also carries `subscribed` and `last_read`.
- New mentions: no dedicated event. A `message` event whose text contains `<@USERID>` is the known signal. `desktop_notification` exists but when Slack sends it is unconfirmed, and no event raises a channel's `mention_count` on arrival.

Rate limits were not hit by back-to-back `client.counts` or `saved.list` calls.

## GitHub

Both sources run `gh auth token` for a token and call the GraphQL API; they poll every 15 seconds, and at once when the panel opens or the Mac wakes. A failed poll keeps the last list. A pull request with no activity (its last-updated time) for more than 14 days is stale and dropped. `roundup-github -bot-login` lists accounts that act for automation but appear on GitHub as ordinary users; they are treated as bots everywhere (unanswered comments, who a task is from).

### Reviews (`github-reviews`)

Main-list tasks for open pull requests that need an answer from the person, from two searches:

- `is:open is:pr user-review-requested:@me archived:false`: a review requested from the person directly (a team request does not count). The task is created at the newest review-request event and links to the pull request.
- `is:open is:pr reviewed-by:@me -author:@me archived:false`: a pull request where someone replied to the person's review thread and the person has not answered, unless the person approved it. The task is created at the reply and links to it.

A request takes precedence over a reply for the same pull request. The row leads with the author's photo and its detail line reads `repo#number · author`. Review threads are read only for pull requests whose last-updated time changed since the last poll.

### Authored (`github-authored`)

Section "Pull requests": the person's own open pull requests, drafts included. It has no count and no due date.

- Lead: failed checks (red cross) take precedence over changes requested (accent `plusminus`), which takes precedence over a comment to answer (accent bubble). Otherwise approved is a green tick and waiting is a grey `ellipsis`.
- Readouts: unanswered comment count (muted bubble), then checks as passed over total with a green tick when all passed, a red cross when any failed and an amber dot while some run. Neutral and skipped checks count as passed; cancelled, timed-out and any other state count as failed.
- A comment from a person is answered when the person replied after it, reacted to it with an emoji, or the thread was resolved. Bot comments are ignored.
- A pull request with failing checks has outcome `failed`, so it adds to the menu bar's red number.

### Rate budget

GraphQL allows 5,000 points an hour. A steady poll costs about 2 points for Reviews (two searches) and 1 for Authored, because threads and conversations are re-read only for pull requests whose last-updated time changed. The first poll costs a few more. Re-reading every thread on every poll would use about 26 points and exhaust the budget.

GitHub's live-update socket (`alive.github.com`) is not practical: its channel names are signed by the server and handed out only in page HTML, so a client needs a browser session and page scraping, and the one useful channel is the notification indicator. The notifications REST feed asks clients to wait 60 seconds between checks, webhook forwarding needs admin rights per repository or organisation, and GraphQL has no subscriptions.

## Where data is stored

| What | Where |
|---|---|
| Configuration | `~/.config/roundup/config.json` (or `$XDG_CONFIG_HOME/roundup/config.json`), or `-config <path>` |
| Plugins installed from `package` entries, and `installed.json` recording each one's package and version | `~/Library/Application Support/roundup/plugins/bin/` |
| Dismissals | `~/Library/Application Support/roundup/dismissed.json` |
| Slack session | login Keychain, service `slk`, account `<workspace>` |
| Chrome profile for Slack sign-in | `~/Library/Application Support/slk/chrome-profile/` |
| Slack lookup cache | `~/Library/Caches/roundup/slack-<workspace>.json` |
| Profile photos | `~/Library/Caches/roundup/avatars/` |
| Log, under the launch agent | `~/Library/Logs/roundup.log` |

## Starting at login

`launchd/roundup.plist.in` is a launch agent template. `make install-agent` fills it in, installs it at `~/Library/LaunchAgents/local.roundup.plist` and loads it. It runs `bin/roundup` at login with `/opt/homebrew/bin` on `PATH` so `gh` and `go` are found, restarts it only after a crash, and sends output to the log. After a rebuild, `make restart-agent`; to remove it, `make uninstall-agent`.
