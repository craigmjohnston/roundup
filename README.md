# Roundup

A macOS menu bar app that gathers the things waiting on you into one list. The app shows the list; every source is a separate program that speaks a [small protocol](PROTOCOL.md). This repository ships three: Slack to-dos and saved items, pull requests waiting for your review, and your own open pull requests.

It is built on [MyGo](https://github.com/egoist/mygo).

## Requirements

- macOS and Go 1.27.3 or later. Go must be on the PATH of the app to install plugins from `package` entries; the launch agent puts `/opt/homebrew/bin` and `/usr/local/bin` there.
- [`gh`](https://cli.github.com), signed in with `gh auth login`, for the GitHub plugin. It uses gh's token.
- Google Chrome, for the Slack plugin's sign-in. The plugin opens Chrome on your workspace's sign-in page and keeps the session it gets.

## Build and run

```
make build
bin/roundup
```

`make build` puts the app and every plugin in `bin/`:

- `bin/roundup`: the app.
- `bin/roundup-slack`: the Slack plugin.
- `bin/roundup-github`: the GitHub plugin.
- `bin/slk`: a command-line client for the Slack session, used to try out Slack calls and events (`bin/slk --help`).

The app runs the sources named in its configuration file, described below. With no configuration file it runs none, and the panel says where to put one.

`make build` first copies MyGo v0.9.0 from the Go module cache into `third_party/mygo` and applies `patches/mygo-v0.9.0-tray-icon.patch`, which lets a coloured menu bar icon keep its own size and have a variant for when the item is highlighted.

## Configuration

The app reads `~/.config/roundup/config.json`, or `$XDG_CONFIG_HOME/roundup/config.json` when that is set. Pass `-config <path>` to use another file. A file that cannot be parsed stops the app at startup, and the error names the file.

```json
{
  "sources": [
    {"package": "github.com/craigmjohnston/roundup/plugins/slack/cmd/roundup-slack@latest", "args": ["-workspace", "acme"]},
    {"package": "github.com/craigmjohnston/roundup/plugins/github/cmd/roundup-github@latest", "args": ["-bot-login", "deploybot", "reviews"]},
    {"package": "github.com/craigmjohnston/roundup/plugins/github/cmd/roundup-github@latest", "args": ["-bot-login", "deploybot", "authored"]}
  ]
}
```

`sources` is the only setting. Each entry is a program the app starts, and has exactly one of `command` or `package`. The app starts them together, lists them in the order given, restarts one 30 seconds after it exits, and talks to it over its standard input and output. A file that has a `slack` or `github` key stops the app at startup: those services run as the plugins above.

A `package` entry is a Go import path and a version, `<import path>@<version>`, with the program's `args`. The app runs `go install` for it and then runs the binary, so it needs `go` on its PATH. Without Go the entry is skipped and the log says so.

- The binary goes in `plugins/bin` under the user config directory (`~/Library/Application Support/roundup/plugins/bin`), named after the last element of the import path, beside `installed.json`, which records what was installed.
- A pinned version, such as `@v1.2.0`, installs only when the binary is missing or was installed from a different package or version. Anything other than `latest` counts as pinned.
- `@latest` runs `go install` at every start. If that fails and an earlier install exists, the app logs the failure and runs the earlier binary. If there is none, the entry is skipped.
- The installs run together, and entries that name the same package share one.
- `go install` runs with the app's environment, so your `go env` settings apply, with `GOBIN` set to the directory above and `GOWORK=off`.

A private plugin repository needs `GOPRIVATE` set, for example `go env -w GOPRIVATE=github.com/acme/*`, and git credentials for GitHub that work without a prompt, because the app runs `go install` with no terminal.

A `command` entry runs a program you already have: an absolute path and its arguments. Use it for a local build or a plugin written in another language:

```json
{
  "sources": [
    {"command": ["/path/to/roundup/bin/roundup-slack", "-workspace", "acme"]},
    {"command": ["/path/to/roundup/bin/roundup-github", "-bot-login", "deploybot", "reviews"]}
  ]
}
```

- `roundup-slack -workspace <name>` is the Slack source. `<name>` is your Slack subdomain, as in `acme.slack.com`.
- `roundup-github [-bot-login <login>]... reviews` lists pull requests waiting for your review, and `authored` lists your own open pull requests. Each is its own program, so list both to get both. `-bot-login` names an account that acts for automation but appears on GitHub as an ordinary user; its comments are not counted as comments from a person. Repeat the flag for more than one account.

[PROTOCOL.md](PROTOCOL.md) describes the protocol, for writing another source.

## Start at login

```
make install-agent
```

This installs a launch agent at `~/Library/LaunchAgents/local.roundup.plist` that starts `bin/roundup` at login and logs to `~/Library/Logs/roundup.log`. After a rebuild, run `make restart-agent`. To remove it, run `make uninstall-agent`.

## Where it keeps things

- The Slack session: in the login Keychain, under the service `slk`.
- The Chrome profile used to sign in to Slack: `~/Library/Application Support/slk/chrome-profile/`
- Dismissed tasks: `~/Library/Application Support/roundup/`
- Plugins installed from `package` entries: `~/Library/Application Support/roundup/plugins/bin/`
- Caches (Slack lookups, profile photos): under `~/Library/Caches`

## Tests

```
make test
```

It runs the tests, `go vet` and `gofmt` for the app and for the plugins module.

`NOTES.md` describes how the app works and what was found about each service's API. `AGENTS.md` has the rules for working on the code.
