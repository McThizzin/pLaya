# pLaya 
**Laya Playground**

A terminal client for [`laya serve`](https://github.com/monatis/ggmlc/releases/latest),
built with Bubble Tea and Lip Gloss. It does not load models: it talks to the
HTTP API of a running server and renders the decisions.

Status: **usable playground.** It connects, lists the ten built-in presets, runs
one against the server, and shows the answers with a length guard and a
telemetry bar. The screen is organized as nine persistent tabs: `pLaya`, State,
Questions, Batch, Bench, Compare, Snippets, History and Server. Switching tabs
does not stop a background job; the tab bar marks it with `*`. The State tab
edits a preset's state and the Questions tab edits its questions; both keep a
per-preset draft that runs use directly. Run history, a batch runner, A/B
comparison and a benchmark/cost pane cover repeated and comparative runs. The
Server tab is a Launchpad that can spawn, attach to and stop a `laya serve`
process. Reusable states live in a snippet library, and windowed mode runs a
long document in overlapping, token-bounded pieces.

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
| `-laya` | `laya` (env `LAYA_BIN`) | CLI used for the route preview and the Launchpad |
| `-models-dir` | `~/models/Laya/models` (env `LAYA_MODELS_DIR`) | `--models-dir` for a Launchpad spawn |
| `-device` | `vulkan` (env `LAYA_DEVICE`) | `--device` for a Launchpad spawn |
| `-no-warmup` | off | skip the warm-up request after connecting |
| `-check` | off | run the connectivity check and exit |

## Keys

The nine tabs are persistent workspaces. `alt+0`…`alt+8` switch tabs from
anywhere, even while typing. Bare `0`…`8` do the same when no text field or
modal has focus. `tab`/`shift+tab` cycle tabs except where the current screen
binds `tab` (the State and Compare tabs). A `*` on a tab means its job is still
running in the background.

`ctrl+h` opens one scrollable help overlay, identical from every tab.
`alt+0`—or the layered `esc`—returns to the pLaya home tab. `ctrl+c` always
quits; bare `q` quits unless you are typing or answering a delete confirmation.
`esc` closes a modal or form first, then cancels the active job, then leaves
text focus, and only then goes home.

| Key | Action |
| --- | --- |
| `alt+0`…`alt+8` | switch to pLaya, State, Questions, Batch, Bench, Compare, Snippets, History, Server |
| `0`…`8` | the same, when not typing or in a modal |
| `tab` / `shift+tab` | cycle tabs where the screen does not bind `tab` |
| `ctrl+h` | open or close the help overlay |
| `ctrl+c` | quit |
| `q` | quit when not typing and not confirming |
| `esc` | close a modal, cancel the active job, leave text focus, then go home |
| `f` | cycle the model family for the current tab |
| `R` | reload or reset the current tab |
| `[` / `]` | keep their per-screen roles (window size, benchmark runs) |

### pLaya home tab

| Key | Action |
| --- | --- |
| `up` / `down`, `k` / `j` | select a preset |
| `g e x t m r s i c h` | jump to guard, email, expense, triage, moderation, router, security, invoice, customer_service, harness |
| `enter` | run the selected preset's edited draft |
| `v` | toggle the raw JSON response |
| `R` | reconnect, reload presets and health (drops drafts) |
| `ctrl+r` | reset the selected preset's draft to the server copy |
| `ctrl+e` | open the State tab |
| `ctrl+b` | open the Bench tab |

### State tab (`alt+1`, or `ctrl+e` from pLaya)

The text tab is a large textarea for `state[state_key]`. `tab` opens the fields
tab, where `a` adds a key/value pair, `e`/`enter` edits the selected one,
`d` deletes it, and `ctrl+r` edits the whole state as raw JSON (`ctrl+s`
parses and applies). `ctrl+f` (or `f` when the canvas is not focused) cycles
the model family without leaving the editor. The length meter updates as you
type and names the longest sequence; `esc` first pauses text focus, and a
second `esc` returns to the pLaya home tab
(`ctrl+s` applies and returns immediately), keeping the draft. `R` reverts the
state to the server copy. After a short pause the canvas also runs
`laya detect-lang` and shows the likely route, reason and script; that route
feeds the length guard while the model is on `auto`. `ctrl+l` opens the snippet
library and `ctrl+w` opens windowed mode with the text currently in the canvas.

### Snippets tab (`alt+6`, or `ctrl+l` from the State tab)

Saves the whole state object, its `state_key` and the source preset under
`os.UserConfigDir()/playa/snippets` (`~/.config/playa/snippets` on Linux, mode
`0600`). `j`/`k` move, `enter` replaces the current draft's state and key with
the selected snippet, `s` saves the current draft under a name, `d` then `y`
deletes, `v` shows the stored JSON, `R` refreshes the list from disk, and `esc`
goes back. Loading marks the draft dirty and refreshes the State tab textarea.
A snippet saved from one preset can be loaded into another; its `state_key`
replaces the draft's key.

### Windowed mode (`ctrl+w`)

Splits only the main text field into overlapping, token-bounded windows and
runs the draft's questions once per window, sequentially. The screen starts at
32 estimated tokens per window; `[`/`]` change the size by 16 tokens, `o`
cycles the overlap (0%, 10%, 25%, 50%), `enter` runs, `j`/`k` select a window,
`v` shows the raw response, `R` re-splits with the current state, and `esc`
cancels a run or returns to the State tab. The window with the highest mean
winner probability is marked
`★ strongest` after the run. Every completed window is recorded in run history
and feeds the latency predictor. The run uses a snapshot, so later canvas edits
cannot change it.

### Questions tab (`alt+2`)

`j`/`k` move, `n` adds a question (pick choice, score or noul), `e`/`enter`
edits, `d` deletes, and `J`/`K` reorder. `p` shows the exact payload preview,
`r` edits the question set as raw JSON, `ctrl+s` saves it and `ctrl+o` loads a
saved set. Saved sets live under `os.UserConfigDir()/playa/questions`
(`~/.config/playa/questions` on Linux). Question order and criteria order are
preserved; choice and noul options keep the order you type.

Changes are per-preset drafts. Running a preset (`enter`) uses the draft, not
the server's published copy, and edited presets show a dirty marker. `R` in
the Questions tab reverts the question list to the server copy; `ctrl+r` on
the pLaya home tab resets the whole draft. A server error that names a
`questions.<key>` param is shown with a hint.

### History tab (`alt+7`)

Every completed decide call is stored, including failures. The store keeps the
50 most recent runs and persists them to
`os.UserConfigDir()/playa/history.json` (`~/.config/playa/history.json` on
Linux, mode `0600`). In the History tab `j`/`k` move, `enter` replays the
selected run, `v` toggles the raw JSON response, `R` reloads the store from
disk, and `esc` returns to the pLaya home tab (or cancels an in-flight replay).
A replay re-sends the exact request that was stored, not the current edited
draft. Replayed runs are recorded as
new entries marked `replay of #N`, and the original is never overwritten.
Failed runs keep the request and the error, so a bad payload can be inspected
and retried after the preset or model changes.

### Batch tab (`alt+3`)

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
opens a row (with `v` for the raw responses), `F` cycles filters (all,
contested, saturated, truncated, errors), `f` cycles the model family,
`s`/`S` sort and reverse, and `t` cycles the noul positive threshold. `x`
writes two files next to the input:
`*.results.jsonl` with the full raw responses and `*.results.csv` with one
answer, probability and certainty per question plus family, route, timing and
token columns. The summary strip reports complete/failed rows, p50/p95 latency,
tokens and rows per second, the noul positive rate, choice counts and truncation.

### Bench tab (`alt+4`, or `ctrl+b` from pLaya)

Runs the selected preset's draft N times and summarizes warm latency. The
default is 20 runs after 3 warm-up calls; `[`/`]` change the run count by 5
(5 to 100), `w` cycles the warm-up count (3, 0, 1, 2), and `f` cycles the model
between `auto` and the families from `/health`. `enter` starts a run, `R`
reloads saved results, and `esc` cancels one in progress (keeping completed
warm runs) or returns to the pLaya home tab. Warm-up calls are discarded, and
the run count is saved only after it finishes. The view shows p50, p95, min,
max, mean, ms per question, tokens per second and questions per second, with
the two latest results for the preset side by side so two families or two runs
can be compared.

Results persist to `os.UserConfigDir()/playa/bench.json`
(`~/.config/playa/bench.json` on Linux, mode `0600`) with the timestamp,
family, device, server and run configuration.

After every normal or batch run the result pane adds a cost readout: client
wall time, server latency, question count, ms per question, input/output
tokens, tokens per second, questions per second and a predicted latency with
its source. The prediction starts from built-in defaults fitted to this GPU.
Warm calls from normal runs, batch rows and benchmarks feed a rolling
per-family fit; after 8 warm samples the fit replaces the defaults for that
family, falling back to a global fit when a family is still quiet. Cold calls
and failures are ignored. Persisted history seeds the predictor at startup.

### Compare tab (`alt+5`)

Runs the selected preset's draft against two targets and marks the questions
whose winners differ. A target is a server URL plus an optional pinned family.
Both sides start on the current server: target A keeps the model selected in
the pLaya tab and target B starts on the first family from `/health`, so `enter`
immediately compares routed and pinned answers. Use two URLs to compare builds
or quants on two servers.

`tab` selects a target, `f` cycles its family (`auto` or one from `/health`),
`u` edits its URL, and `R` resets the URL to the primary server. Both requests
run concurrently, so a failure on one side does not hide the other. The result
view shows each target's server latency, wall time, route, token counts and
answers side by side (stacked on narrow terminals), followed by the list of
disagreements. `esc` cancels a run, or returns to the pLaya home tab when idle.
Completed A/B calls are recorded in run history and feed the latency
predictor like any other run.

### Server tab (`alt+8`)

Starts and stops a `laya serve` child process from the TUI. `j`/`k` select a
field, `e`/`enter` edit it, `s` starts, `x` stops, `a` attaches to a server
that is already listening, `w` sends one tiny request to every family from
`/health` so the first real call does not pay the model-load cost, and `R`
re-checks health. `esc` returns to the pLaya home tab; quitting from anywhere
stops a child this session spawned.

The editable fields are the `laya` binary, models dir, port, device, max batch
and family. Max batch is omitted when empty, and `auto` omits `--family`. Start
builds the command from the documented
`laya serve --models-dir ... --port ... --device ...` form and polls
`/health` every 500 ms for up to 30 seconds. The screen shows the target URL,
the child PID and its state, plus the tail of the child's stdout/stderr.

The manager owns only the process it spawned. Attach (`a`) checks a URL and
switches the client without stopping anything, so an external `laya serve` is
never killed. `x` refuses to touch it and says so. On Unix the child gets its
own process group, so interrupt (then kill after a 3 second grace period) also
takes down helpers it started. `SIGINT`/`SIGTERM` are routed through Bubble
Tea's `Quit`, and `Close` stops every manager the session created.

## Layout

```
cmd/playa/            flags, wiring
internal/api/         stdlib-only client + types (ordered questions, errors); fixtures in testdata/
internal/batch/       batch file loaders, question chunking, row checks, summary, filters, exports
internal/bench/       latency predictor, benchmark stats, bounded result store
internal/forge/       ordered question sets, validation, save/load under the user config dir
internal/history/     bounded run history, deep-copied entries, atomic JSON persistence
internal/langdetect/  `laya detect-lang` runner and output parser for the route preview
internal/launch/      owned `laya serve` child, bounded logs, process-group stop
internal/lens/        answer -> winner, runner-up gap, certainty, contested/saturated flags
internal/lengthguard/ token estimate, per-family caps, silent-truncation detection
internal/snippet/     reusable state snapshots under the user config dir
internal/window/      overlapping, token-bounded text splitter
internal/telemetry/   rolling median per request kind, cold calls excluded
internal/check/       the -check connectivity report
internal/tui/         Bubble Tea model, tabbed shell, draft state, tab views
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
the tab shell, State and Questions tabs, snippet library, windowed mode,
history/replay, batch runner, bench/cost, Compare and Server flows, so they
cover request payloads without a terminal.

It has been live-smoke-tested against a running four-family `laya serve`:
`/health`, `-check`, a decide through the TUI, snippet save and load, and a
10-window windowed run. `make check` is still the quickest first step; then
`./bin/playa` against
`laya serve --models-dir ~/models/Laya/models/ --port 8080 --device vulkan`.

## Not built yet (see the spec's build order)

- Spawning `laya daemon` (the Launchpad starts `laya serve` only)
- Separate single/preset medians are tracked but only presets can be run so far
- A real big-letter banner for the harness preset (spaced capitals for now)
