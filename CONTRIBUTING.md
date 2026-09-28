# Contributing to Tandem

Thanks for your interest in improving Tandem! It's a small, self-hosted Go
app, so contributing is deliberately low-ceremony.

## Getting set up

You need [Go 1.25+](https://go.dev/dl/). There's no Node or frontend build
step.

```sh
git clone https://github.com/jwald3/Tandem.git && cd Tandem
go run . -seed-demo -db dev.db   # first run: a dev database with fake data
go run . -db dev.db              # later runs: reuse it
```

Then open <http://localhost:8090>. Every tab works without an Anthropic API
key; you only need one to try the chat. See the [README](README.md) for the
full tour, configuration, and how the assistant works.

## Before you open a pull request

Please make sure these all pass locally. CI runs the same checks:

```sh
go test ./...          # unit tests
go vet ./...           # static checks
gofmt -l .             # should print nothing; run `gofmt -w .` to fix
go build ./...         # it compiles
```

The tests need no API key and make no network calls. The assistant's tool-use
loop runs against a fake Messages API (`fakeAPI` in
`internal/assistant/assistant_test.go`) that records each request and replies
from a script, so you can test new tools and prompts deterministically.

## Where things live

- **`internal/store`**: the SQLite schema and queries, one file per record
  type. Schema changes to existing tables need a migration step in
  `schema.go`.
- **`internal/assistant`**: the Claude tool-use loop (`agent.go`), the system
  prompt and per-request context (`prompt.go`), and the tools
  (`tools_*.go`). A new tool is a `tool` value added to `allTools` in
  `tools.go`.
- **`internal/server`**: HTTP handlers, one file per tab.
- **`web/templates`**, **`web/static`**: HTML templates, CSS and plain JS.

## A few conventions

- **Keep the stack small.** No JavaScript build step, and no new heavy
  dependencies without a good reason. The app is the Go standard library,
  [HTMX](https://htmx.org), [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
  and the [Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go).
- **The data tabs work without JavaScript.** They're plain forms that post and
  redirect; HTMX is only used on the chat page.
- **Templates and static files are embedded** with `go:embed`, so restart the
  app after editing them.
- **Dependencies point one way:** `server` and `assistant` use `store`, never
  the reverse.
- **Tool results should tell Claude what to do next.** When a tool fails (an
  ambiguous contact name, a bad date), return an error that says how to fix
  it, not just that it failed.
- **Add a test** next to the code you change (`*_test.go`).
- **Use made-up names** in tests, demo data and examples.
- **Never commit database files** (`*.db`, WAL files, backups). They hold
  personal data and possibly an API key. They're git-ignored.

## Reporting bugs and requesting features

Open an issue. For bugs, the template asks for steps to reproduce, what you
expected, your OS, and how you're running the app (`go run`, a built binary,
or Docker). That context makes a big difference.

## Security

Please don't file security issues in the public tracker. See
[SECURITY.md](SECURITY.md).
