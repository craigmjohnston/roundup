# Working on this repo

## Checking the UI

Never launch the app, open its panel or take a screenshot: it steals focus from the person using the Mac. Render the panel in memory instead and read the PNGs:

```
ROUNDUP_SNAPSHOT_DIR=/some/dir go test ./cmd/roundup -run TestSnapshots -count=1
ROUNDUP_SNAPSHOT_SCALE=1 ROUNDUP_SNAPSHOT_DIR=/some/dir go test ./cmd/roundup -run TestSnapshots -count=1
```

The first renders at 2x, the second at 1x. Judge small UI at 1x, at actual size. Only the menu bar placement and the real window need a person to look.

Do not touch the running app, its launch agent or its configuration file.

## Plugins

Slack and GitHub are plugins: separate programs under `plugins/` that speak `PROTOCOL.md`, built with `protocol.Serve`. `cmd/roundup` and the packages it imports must not import `plugins/`; `TestAppDependsOnNoPlugin` checks it. `plugins/` is its own Go module with no `replace`, joined to the root module by `go.work`; run `go mod tidy` in each with `GOWORK=off`. A change to what a plugin shows or sends goes in the plugin, and the app learns it from the hello and the tasks.

## Tests

- While working, run only the tests that cover the change: one test, file or package (`go test ./plugins/github -run TestAuthored`).
- Run `make test` (tests, vet, gofmt) once at the end, or after a really significant change.
- Tests use fakes and `httptest` through the helpers already in each package. A plugin's end-to-end test runs its source through `protocol.ServeSource` and `protocoltest`. Add subtests to a test that already sets up the state you need before writing a new one.
- Test data uses fictional names (`acme`, `widgets#7`), never real people, repositories or workspaces.

## Comments

Default to none. Add one only for a non-obvious reason, a vendor quirk or where a hard-coded value came from, as one plain sentence in the present tense. Never narrate history or restate the code.

## Commits

Prefix the subject with `roundup:`.
