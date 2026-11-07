# pLaya 
**Laya Playground**

A terminal client for [`laya serve`](https://github.com/monatis/ggmlc/releases/latest),
built with Bubble Tea and Lip Gloss. It does not load models: it talks to the
HTTP API of a running server and renders the decisions.

Status: **usable playground.** It connects, lists the ten built-in presets, runs
one against the server, and shows the answers with a length guard and a
telemetry bar. The State Canvas edits a preset's state and the Question Forge
edits its questions; both keep a per-preset draft that runs use directly.

## Quick start

```sh
make build

# in another terminal
laya serve --models-dir ~/models/Laya/models --device vulkan --port 8080

./bin/playa -check                            # plain-text connectivity check, no TUI
./bin/playa                                   # the TUI
```

Dependencies are pinned in `go.mod` (Bubble Tea, Lip Gloss, bubbles, huh). The
module is named `playa`; rename it with `go mod edit -module` (and update the
imports) if you publish it.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-url` | `http://127.0.0.1:8080` (env `LAYA_URL`) | where `laya serve` listens |
| `-api-key` | env `LAYA_API_KEY` or `TYPESAFE_API_KEY` | sent as `Authorization: Bearer ...` |
| `-endpoint` | `systemone` | `systemone` or `decide` (same response in the one case checked) |
| `-timeout` | `30s` | per request; the first call to a family may load a model |
| `-laya` | `laya` (env `LAYA_BIN`) | CLI used for the route preview |
| `-no-warmup` | off | skip the warm-up request after connecting |
| `-check` | off | run the connectivity check and exit |

## Keys

| Key | Action |
| --- | --- |
| `up` / `down`, `k` / `j` | select a preset |
| `g e x t m r s i c h` | jump to guard, email, expense, triage, moderation, router, security, invoice, customer_service, harness |
| `enter` | run the selected preset's edited draft |
| `esc` | cancel the in-flight request |
| `ctrl+e` | open the State Canvas |
| `ctrl+q` | open the Question Forge |
| `H` | open run history (uppercase; `h` jumps to harness) |
| `B` | open the batch runner (uppercase) |
| `f` | cycle the pinned model family (`auto` or one from `/health`) |
| `R` | reset the selected preset's draft to the server copy |
| `v` | toggle the raw JSON response |
| `ctrl+r` | reconnect and reload presets (drops drafts) |
| `q`, `ctrl+c` | quit |

### State Canvas (`ctrl+e`)

The text tab is a large textarea for `state[state_key]`. `tab` opens the fields
tab, where `a` adds a key/value pair, `e`/`enter` edits the selected one,
`d` deletes it, and `ctrl+r` edits the whole state as raw JSON (`ctrl+s`
parses and applies). `ctrl+f` cycles the model family without leaving the
editor. The length meter updates as you type and names the longest sequence;
`esc`/`ctrl+s` returns to the browser and keeps the draft. After a short pause
the canvas also runs `laya detect-lang` and shows the likely route, reason and
script; that route feeds the length guard while the model is on `auto`.

### Question Forge (`ctrl+q`)

`j`/`k` move, `n` adds a question (pick choice, score or noul), `e`/`enter`
edits, `d` deletes, and `J`/`K` reorder. `p` shows the exact payload preview,
`r` edits the question set as raw JSON, `ctrl+s` saves it and `ctrl+o` loads a
saved set. Saved sets live under `os.UserConfigDir()/playa/questions`
(`~/.config/playa/questions` on Linux). Question order and criteria order are
preserved; choice and noul options keep the order you type.

Changes are per-preset drafts. Running a preset (`enter`) uses the draft, not
the server's published copy, and edited presets show a dirty marker. `R` on
the browser discards the draft. A server error that names a `questions.<key>`
param is shown with a hint, and `ctrl+q` jumps to that question.

### Run history (`H`)

Every completed decide call is stored, including failures. The store keeps the
50 most recent runs and persists them to
`os.UserConfigDir()/playa/history.json` (`~/.config/playa/history.json` on
Linux, mode `0600`). In the history screen `j`/`k` move, `enter` replays the
selected run, `v` toggles the raw JSON response, and `esc` returns to the
browser (or cancels an in-flight replay). A replay re-sends the exact request
that was stored, not the current edited draft. Replayed runs are recorded as
new entries marked `replay of #N`, and the original is never overwritten.
Failed runs keep the request and the error, so a bad payload can be inspected
and retried after the preset or model changes.

### Batch runner (`B`)

Runs a file of states through the selected preset's questions and shows every
result in one table. `ctrl+o` opens a path prompt (`.jsonl`/`.ndjson` or
`.csv`). JSONL has one state per line: a string or an object. CSV turns every
column into a state field and uses the preset's `state_key` column for the row
label. The question set is snapshotted when the screen opens; later edits do
not change a run in progress.

Questions are sent in chunks of 8 (`max_batch`), so a larger set costs one
extra request per state. Two rows run concurrently; `r` starts or resumes the
incomplete rows, `R` re-runs everything, and `esc` cancels the in-flight run
without discarding finished rows. `j`/`k` move, `h`/`l` scroll columns, `enter`
opens a row (with `v` for the raw responses), `f` cycles filters (all,
contested, saturated, truncated, errors), `s`/`S` sort and reverse, and `t`
cycles the noul positive threshold. `x` writes two files next to the input:
`*.results.jsonl` with the full raw responses and `*.results.csv` with one
answer, probability and certainty per question plus family, route, timing and
token columns. The summary strip reports complete/failed rows, p50/p95 latency,
tokens and rows per second, the noul positive rate, choice counts and truncation.

## Layout

```
cmd/playa/            flags, wiring
internal/api/         stdlib-only client + types (ordered questions, errors); fixtures in testdata/
internal/batch/       batch file loaders, question chunking, row checks, summary, filters, exports
internal/forge/       ordered question sets, validation, save/load under the user config dir
internal/history/     bounded run history, deep-copied entries, atomic JSON persistence
internal/langdetect/  `laya detect-lang` runner and output parser for the route preview
internal/lens/        answer -> winner, runner-up gap, certainty, contested/saturated flags
internal/lengthguard/ token estimate, per-family caps, silent-truncation detection
internal/telemetry/   rolling median per request kind, cold calls excluded
internal/check/       the -check connectivity report
internal/tui/         Bubble Tea model, draft state, State Canvas, Question Forge, views
docs/SERVER-NOTES.md  what was verified against a real server
```

All decision logic lives in `forge`, `lens`, `lengthguard` and `telemetry`, so
it is testable without a terminal. The `tui` package only wires it to the
screen.

## What the fixtures are

`internal/api/testdata/` is the single canonical fixture location. It holds
responses captured from a real server (Vulkan, RX 580): `presets.json`,
`score.json`, `noul_no.json`, the run captures under `run_*.json`, plus
`health`, `models`, `noul`, `choice` and the error body reconstructed from
captured output. The TUI tests use small **synthetic** decide responses
(marked in the test file) shaped like real ones.

## Verification status

`go vet ./...`, `go test ./...` and `go test -race ./...` pass with the pinned
dependencies. The TUI tests drive the model through a fake server, including
the State Canvas, Question Forge, history/replay and batch-runner flows, so
they cover request payloads without a terminal.

It has **not** been run against your live four-family server yet. `make check`
is the first thing to try; then `./bin/playa` against
`laya serve --models-dir ~/models/Laya/models/ --port 8080 --device vulkan`.

## Not built yet (see the spec's build order)

- Snippet library and windowed mode
- A/B compare, benchmark pane
- Spawning `laya serve` / `laya daemon` and the Launchpad
- Separate single/preset medians are tracked but only presets can be run so far
- A real big-letter banner for the harness preset (spaced capitals for now)
