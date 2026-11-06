# pLaya 
**Laya Playground**

A terminal client for [`laya serve`](https://github.com/monatis/ggmlc/releases/latest),
built with Bubble Tea and Lip Gloss. It does not load models: it talks to the
HTTP API of a running server and renders the decisions.

Status: **early scaffold.** It connects, lists the ten built-in presets, runs
one against the server, and shows the answers with a length guard and a
telemetry bar. Editing states and questions is not built yet.

## Quick start

```sh
go mod tidy                                   # fetches Bubble Tea and Lip Gloss, writes go.sum
make build

# in another terminal
laya serve --models-dir ~/models/Laya/models --device vulkan --port 8080

./bin/playa -check                            # plain-text connectivity check, no TUI
./bin/playa                                   # the TUI
```

`go.mod` deliberately has no `require` lines: `go mod tidy` resolves current
versions. The module is named `playa`; rename it with `go mod edit -module`
(and update the imports) if you publish it.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-url` | `http://127.0.0.1:8080` (env `LAYA_URL`) | where `laya serve` listens |
| `-api-key` | env `LAYA_API_KEY` or `TYPESAFE_API_KEY` | sent as `Authorization: Bearer ...` |
| `-endpoint` | `systemone` | `systemone` or `decide` (same response in the one case checked) |
| `-timeout` | `30s` | per request; the first call to a family may load a model |
| `-no-warmup` | off | skip the warm-up request after connecting |
| `-check` | off | run the connectivity check and exit |

## Keys

| Key | Action |
| --- | --- |
| `up` / `down`, `k` / `j` | select a preset |
| `g e x t m r s i c h` | jump to guard, email, expense, triage, moderation, router, security, invoice, customer_service, harness |
| `enter` | run the selected preset |
| `v` | toggle the raw JSON response |
| `ctrl+r` | reconnect and reload presets |
| `q`, `ctrl+c` | quit |

## Layout

```
cmd/playa/            flags, wiring
internal/api/         stdlib-only client + types (ordered questions, errors); fixtures in testdata/
internal/lens/        answer -> winner, runner-up gap, certainty, contested/saturated flags
internal/lengthguard/ token estimate, per-family caps, silent-truncation detection
internal/telemetry/   rolling median per request kind, cold calls excluded
internal/check/       the -check connectivity report
internal/tui/         Bubble Tea model, messages/commands, view
docs/SERVER-NOTES.md  what was verified against a real server
```

All decision logic lives in `lens`, `lengthguard` and `telemetry`, so it is
testable without a terminal. The `tui` package only wires it to the screen.

## What the fixtures are

`internal/api/testdata/` holds responses captured from a real server (Vulkan,
RX 580): `presets.json`, `score.json`, `noul_no.json` as files, and `health`,
`models`, `noul`, `choice` and the error body reconstructed from captured
output. The TUI tests use small **synthetic** decide responses (marked in the
test file) shaped like real ones.

## Verification status

Tests run: `go vet`, and `go test -race` on every package, plus a scripted run
of the binary in a pseudo-terminal against a fake server. This was done in a
sandbox with no module proxy, so it was **compiled against the distro-packaged
Bubble Tea v0.19 and Lip Gloss v0.5**, not current releases. The code sticks to
API that has not changed, with one deliberate difference: `main.go` uses
`p.Run()`, which current versions have. After `go mod tidy`, run
`go vet ./... && go test ./...` and report anything that fails.

It has **not** been run against your live server yet. `make check` is the
first thing to try.

## Not built yet (see the spec's build order)

- Editable state (State Canvas) and the question forms (Question Forge, `huh`)
- Run history, batch runner, A/B compare, benchmark pane
- Spawning `laya serve` / `laya daemon` and the Launchpad
- Family choice in the length guard (it assumes `english` until a response
  reports the real family), and separate single/preset medians are tracked but
  only presets can be run so far
- A real big-letter banner for the harness preset (spaced capitals for now)
