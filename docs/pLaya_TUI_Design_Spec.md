# pLaya: Design Spec

A Go (Bubble Tea + Lip Gloss) terminal client for `laya serve`. The TUI does not load models; it is a fast client for the local HTTP API. Everything marked **verified** was observed on a real server (Vulkan, AMD RX 580, no fp16), not taken from docs.

## 1. Architecture

- **Engine:** `laya serve --models-dir models --port 8080 --device vulkan`, one process, multiple model families.
- **Client (Go):** `net/http`, all calls wrapped in `tea.Cmd` so the UI never blocks. Requests are cancellable with `context.Context`.
- **UI:** Bubble Tea for state, `bubbles` (textarea, list, viewport), `charmbracelet/huh` for the question forms, Lip Gloss for styling.
- **Fixtures:** keep the captured JSON responses in `internal/api/testdata/` (the single canonical location) and test the client with `httptest` and the TUI by driving the model directly.

## 2. API contract (verified)

### Endpoints

| Endpoint | Notes |
| --- | --- |
| `GET /health` | `status`, `device` (`vulkan:0`), `families` (catalog), `family` (`auto`), `model`, `last_route`, `last_reason`. The last two are global to the server, not per request. |
| `GET /v1/models` | 9 names: 4 real families (`english`, `multilingual`, `typed-decisions`, `kev-0.8b`) plus aliases (`laya`, `jev-latest`, `laya-multilingual`, `laya-typed-decisions`, `jaredpalmer/kev-0.8b`). Dedupe in the picker. |
| `GET /v1/presets` | 10 presets, each ready to POST: `name`, `title`, `blurb`, `state_key`, `state`, `questions`. |
| `POST /v1/decide` | Alias of /v1/systemone; same response in the one case checked. Optional `model` field pins a family. |

Checked: `/v1/systemone` returns the same response as /v1/decide for the same noul request (checked once). The help names /v1/systemone as the documented TypeSafe endpoint (what the Studio and typesafe-sdk use), so make it the client default and keep /v1/decide as an alias. The batch endpoint is untested.

### Question shapes

- **noul:** `instructions`; `criteria` optional, as `{"false": "...", "true": "..."}`.
- **choice:** `instructions`; `criteria` required, a map of key to description (description may be `null`). Max 16 options.
- **score:** `instructions`; `criteria` required, an **ordered array** of level descriptions (min 2). A level's index is its score. There are no min/max bounds.

Presets reference state keys in backticks inside instructions (for example ``Which team should handle the email in `body`?``).

### Answer shapes

```json
// noul
{"type":"noul","action":{"act_probability":1},"confidence":0.9196,"noul":0.9196}
// choice
{"type":"choice","action":{...},"confidence":0.7902,"choice":"angry","probabilities":{"calm":0.0331,"angry":0.9669}}
// score
{"type":"score","action":{...},"confidence":0.6883,"score":1.8894,"legend":{"0":"can wait","1":"this week","2":"today"},"probabilities":{"0":0.0258,"1":0.0589,"2":0.9152}}
```

Top level: `model`, `family`, `route`, `answers`, `usage` (`input_tokens`, `output_tokens`, `latency_ms`).

Errors: `{"error":{"code":"invalid_request_error","message":"...","param":"questions.urgency.criteria"}}`. `param` is a field path, so map it onto the form field.

### What the numbers mean

- `noul` is P(yes). Its `confidence` is max(p, 1-p), the probability of whichever answer won.
- For choice and score, `confidence` is 1 minus the normalized Shannon entropy of the probabilities. A near-flat distribution gives a very low value (harness `next` came back at 0.0787).
- These two scales are not comparable. A 65/35 noul shows 0.65; a 65/35 two-way choice shows about 0.07.
- `score` is the probability-weighted level index, 0 to n-1.
- `action.act_probability` was 1 in every answer of the ten built-in presets, but a Kev noul answer returned 0 (`internal/api/testdata/noul_kev.json`). Meaning unknown; keep it in the raw view only.
- Probabilities can saturate to exactly 0 or 1 (4-decimal rounding).

## 3. Limits and behavior (verified)

| Topic | Finding |
| --- | --- |
| Startup line | `max_len=512 max_opts=16 max_batch=8`. Parse it if the TUI spawns the server. |
| Token cap | 512 (english), 1024 (multilingual). Kev: 2048 (confirmed by laya info). |
| Truncation | **Silent. The tail is dropped**: text placed at the end gave the same answer as no text at all. No error, no warning. |
| Dilution | A key sentence at the start of 1000+ tokens of filler dropped from near-certain to about 0.35. Long inputs lose accuracy before the cap. |
| `input_tokens` | The **sum across questions** (each question gets its own sequence of state plus question; presets ran 72 to 104 tokens per question). A state that overflows shows as `n_questions x cap`. |
| Batching | About 25 ms per question up to 8, then a step of roughly +230 ms at 9 (a second forward pass). Flat from 9 to 12. 9+ questions work. |
| Latency (Laya, warm) | Single noul about 60 ms. Presets 200 to 550 ms over HTTP (the email preset, 7 questions, read 551 ms there but 359 ms steady in laya bench, so the HTTP figures include cold calls). Kev about 100 ms wall. |
| Cold calls | The first call after an idle pause was slow several times (250 to 650 ms). Cause unproven. |
| Lazy loading | Only one model loaded at startup; the first Kev call took 1.36 s, then about 100 ms. Very likely lazy per-family loading (confirm via the server log). |
| Routing | Auto mode: `english words 5/8`, `latin but weak english 1/572` (routed to multilingual). Pinned: `model english`. Keyword-light English can flip to multilingual. |
| Catalog | If two GGUFs share a family (english q8\_0 and f16), the server picks one (f16 here). Compare quants with two servers on two ports. |

**Measured with `laya bench` and `laya info`:**

- **Second forward confirmed:** the email preset on Vulkan takes 358.6 ms mean (7 questions, 648 tokens, B=7, S=124, 1 forward): 51.2 ms per question, 19.5 questions per second. With `--max-batch 4` it needs 2 forwards and takes 502.7 ms (+144 ms).
- **Padding:** `laya info` reports `pad: max-in-batch`, so every question in a forward is padded to the longest one. Email has 648 real tokens but 7 x 124 = 868 padded slots.
- **English (Laya):** max\_len 512, head\_max\_len 192, min\_seq 64, size buckets 64, 128, 256, 512. Template: `[CLS] {qtype} question: {instructions} [SEP] ([MASK] {option})* [SEP] {state} [SEP]`, with the question and options before the state.
- **Kev:** max\_len 2048, head\_max\_len 2048, min\_seq 64, size buckets 64 to 2048, vocab 248,077, template state first and then the instructions and options. With a state of about 4,000 tokens and two different noul questions, input\_tokens was 4096 (2 x 2048) and the answers were 0.9785 (refund asked) and 0.0813 (cooking asked), so the question survives truncation.
- **Calibration:** temperatures are baked into the weights and depend on question type and option count. English: noul 1.98; choice 1.91 (2 options), 1.76 (3 to 5), 1.00 (6 to 10), 0.10 (11+); score 1.25 (3 to 5 levels). Kev has three temperatures, all 2.406. A client cannot tune them.
- **Startup line:** `[laya] ready  kind=laya  recipe=laya-compat  max_len=512 max_opts=16 max_batch=8 dynamic=b,s vocab=50368` is printed by serve, bench, daemon and info. Parse it when spawning.
- **Info and Vulkan:** `laya info` works with `--device vulkan`, although its help lists only auto, cpu, cuda and metal. Given `--models-dir` it lists the router families and loads one model (Kev here); pass a GGUF path to inspect a specific one.

## 4. Modules

### 4.1 Launchpad (server and routing)

*(implemented: `L` opens a form for binary, models dir, port, device, max batch and family; `s` spawns `laya serve`, polls `/health` every 500 ms for up to 30 seconds, and tails bounded output; `x` stops only the owned child; `a` attaches to an external server and never stops it; `w` warms every family from `/health`; Unix children get their own process group so Stop also takes down helpers. Not implemented: `laya daemon` transport, per-family `catalogued/loading/ready` state, multi-instance spawn, and forwarding a `-api-key` flag to the child; the environment, including `LAYA_API_KEY`, is inherited.)*

- Spawn with `exec.Command`; poll `/health` for readiness; stream stdout and stderr to a log pane; handle signals so no orphaned GPU process remains.
- **Attach mode:** detect an already-running server and never kill it on exit.
- Pass `LAYA_API_KEY` through if set.
- **Per-family state:** `catalogued, loading, ready`. First call to a family uses a long timeout (15 s or more). A **warm all families** action sends a tiny request to each family.
- Model switching is the `model` field, not a restart. Show a routing pill with `family` and `route`, and a one-key pin to a family. Warn when `route` contains "weak".
- Multi-instance spawn on different ports (for quant A/B compare).
- Telemetry bar: device, family, rolling p50 latency over about 10 calls (single question and full preset tracked separately), server latency vs client wall time. Cold calls (idle more than about 10 s) are marked and excluded from the median.

**Spawn options (from `laya help`):**

- **Transport:** `laya serve` gives the HTTP API plus the Decision Studio web page at `GET /`. `laya daemon` speaks newline-delimited JSON on stdin and stdout, which ties the engine's lifetime to the TUI process (no port, no orphan). Checked: it prints {"status":"ready","model":"laya"} once loaded, then answers each request line (same body as HTTP) with the same response plus an id field ("0" when none was sent). Keep HTTP as the default, since attach mode and other clients need it, and offer daemon as a spawn option.
- **Family pinning:** `--family` accepts only `auto`, `english`, `multilingual` and `typed-decisions`. Kev is reached through the `model` field of a request, or by serving its GGUF directly.
- **Batch cap:** `--max-batch N` caps questions per forward (default comes from the GGUF, 8 here). Expose it as a launch setting.
- **Device flags:** `--device` accepts `vulkan` and `vulkan:0`. Hide `--cuda-graph` unless the device is CUDA, and `--threads` unless it is CPU.
- **Auth:** the server reads `LAYA_API_KEY` or `TYPESAFE_API_KEY` and expects `Authorization: Bearer <key>`. Give the client a key field and never log it.
- **Preset names without a server:** `laya list-presets` needs no model. With a server running, `/v1/presets` already returns full payloads.

### 4.2 Preset Playground

POST a preset's `state` and `questions` directly. Shortcuts: `g` guard, `e` email, `x` expense, `t` triage, `m` moderation, `r` router, `s` security, `i` invoice, `c` customer\_service, `h` harness. An in-flight request is cancelled with `esc`; server errors that carry `error.param` point at the offending state or question.

Show an ETA before sending (about 25 ms per question plus a fixed cost on this GPU). Warn at 9 or more questions that latency roughly doubles.

### 4.3 Question Forge

- Three form shapes: choice (key plus optional description rows, max 16), score (ordered, reorderable level list, min 2), noul (optional true/false descriptions).
- Lint: warn when an instruction references a backticked key that is not in the state.
- Map server errors to fields using `error.param`. *(implemented: the browser shows the param and `Q` selects the named question)*
- Form / raw JSON toggle with a live payload preview.
- Preserve question order and criteria order (decode with `json.RawMessage`, keep a separate key slice).
- Save and load question sets as files under the user config directory.

* **Head budget:** for the english model `head_max_len` is 192 tokens, which looks like the cap for the question part (instructions plus option descriptions), since the template puts that part first. Warn as the form approaches about 192 estimated tokens. Behavior beyond it is untested.

### 4.4 State Canvas

- One large textarea for `state[state_key]`, plus a small key/value editor for the other fields. Free text and structured state both supported.
- Clipboard paste into the state; snippet library for reusable states. *(implemented: `S` opens the library, `ctrl+l` opens it from the canvas; a snippet stores the full state, `state_key` and source preset under `os.UserConfigDir()/playa/snippets`; `enter` replaces the draft's state and key, `s` saves, `d`/`y` deletes, `v` shows the raw JSON. Not implemented: clipboard integration beyond the terminal's own paste.)*
- **Length guard (mandatory, the server will not warn):** estimate tokens client-side (about 4 chars per token for English, fewer for CJK) per question sequence, including the instructions and criteria. Warn at about 70% of the cap, and show that text beyond the cap is ignored from the end. After a call, flag `input_tokens == n_questions x cap`.
- Optional **windowed mode** for long documents: split the state into windows, run them as one batch, and show the strongest window. *(implemented: `ctrl+w` snapshots the draft, splits only the main text into overlapping token-bounded windows, and runs the questions sequentially; `[`/`]` adjust the size, `o` cycles 0/10/25/50% overlap, and the highest mean winner probability is marked strongest. Each window enters run history and feeds the predictor. Not implemented: sending the windows as one chunked batch request; they are independent calls so progress and cancellation stay visible.)*

* **Route preview (no model needed):** `laya detect-lang --text <STR>` prints key: value lines (`script`, `english`, `words`, `english_hits`, `ratio`, `route`, `reason`) without loading a model. Its `reason` matched the server's `route` string for the same sentence (`english words 5/8`). Run it on a debounced pause while the user types, so the canvas can show the likely family and route before any decide call, and show the weak-English warning early. *(implemented: 350 ms debounce, route feeds the length guard while the model is on auto)*
* **Cost drivers:** the server pads every question in a forward to the longest one (`pad: max-in-batch`), so cost follows questions x the longest sequence, not the sum of tokens. Show the longest sequence in the length meter, and tell the user that one long question slows the whole request.
* **Where truncation lands:** Laya's template puts the question and options first and the state last, so an over-long state loses its tail and the question survives. In a Kev test the question also survived a 4,000-token state (which end of the state Kev cuts was not tested). Length warnings should talk about the end of the state.

### 4.5 Decision Lens

- Color by the winning class's probability, not by the server's `confidence`. Show `confidence` as a number only.
- noul: show polarity ("NO 85%"), not a bare P(yes). choice: winner, runner-up and the gap. score: a bar per level with a marker at the expected value.
- Mark saturated values (exactly 0 or 1).
- **Harness banner:** keyed on the `next` choice (act / tool / ask\_user / stop), with a "CONTESTED" style when the gap is small. The example state produced `tool` with confidence 0.0787, so a loud banner would overstate the decision.
- Always show `route` and `family` next to the result.

* **Calibration caveat:** probabilities are temperature-scaled by question type and option count (english: 0.10 for a choice with 11+ options, 1.76 for 3 to 5). Compare certainty only between questions of the same type and similar option counts.

### 4.6 Batch runner

Run a file of states through one preset or saved question set and review every result in one sortable table. This is the main way to test a question set against many inputs and see where the model is unsure. *(implemented: JSONL/CSV input, 8-question chunks, two concurrent rows, cancel/resume, filters and sorting, JSONL/CSV export, summary strip)*

- **Input:** JSONL (one state per line, a string or an object) or CSV (one column becomes `state_key`, the other columns become extra state fields). The snippet library works as a source too.
- **Request plan:** keep every request at 8 questions or fewer (`max_batch=8`); a 9th question adds about 230 ms for a second pass. A larger set is split into chunks of 8 per state. Start with one `/v1/decide` call per state and a small worker pool (1 to 2); the server has one GPU, so measure before adding workers. The README describes a batch endpoint for many states with shared questions (untested here); adopt it only if it measurably beats the loop.
- **Row checks:** estimate tokens against the cap before sending (warn at about 70%). After the call, `input_tokens == n_questions x cap` marks a truncated row.
- **Results table:** one row per state, one column per question showing the winner and its probability, colored like the Decision Lens. Sort by any column; filter by contested (small gap), saturated (exactly 0 or 1), truncated, errored, or unexpected route. Enter opens a row with its raw JSON.
- **Run control:** progress bar, ETA (from 4.7), cancel through `context`, and resume that skips rows already done after an interruption.
- **Summary strip:** counts per choice answer, noul positive rate at a chosen threshold, p50 and p95 latency, tokens per second.
- **Export:** JSONL with the full raw responses, and a flat CSV (state id, then answer, probability and certainty per question, plus family, route, latency, tokens).
- **Threshold rules:** apply them per row as an extra action column, so a gate can be tested against a labeled file.
- **Disagreement view:** run the same file on two targets (A/B in 4.8) and show only the rows where the winners differ.

### 4.7 Benchmark pane and cost readout

*(implemented: cost readout after normal and batch runs, built-in bench at `ctrl+b`, rolling per-family predictor, saved results. Not implemented: the `laya bench` subprocess wrapper and an explicit ETA in the batch summary.)*

- **Cost readout after every run:** total ms, ms per question, input tokens, tokens per second, questions per second, and predicted vs actual time. Batch runs add states per second and total time.
- **Predicted time:** start from the defaults measured on this GPU (about 70 ms plus 22 ms per question for 2 to 8 short questions; the email preset 359 ms at B=7, S=124; a second forward adds about 140 ms (email with --max-batch 4) to 230 ms (short questions) from the 9th question). Then replace them with a rolling fit of ms against B x S (questions times the longest sequence) per family from the user's own recent calls, since another GPU or model will differ. Cold calls after an idle pause are excluded.
- **Built-in bench:** pick a preset or question set and send it N times (default 20 after 3 warmup calls). Report p50, p95, min, max, ms per question and tokens per second, for one family or two side by side.
- **`laya bench` wrapper:** run the CLI's own benchmark as a subprocess and stream its output into a viewport. It times built-in presets only (no custom questions) and prints plain text, not JSON: wall clock, S, B, forwards and q/s. The format is regular (per-run lines such as run 1/5: wall 359.0 ms (7 q, 648 tok S=124 B=7 forwards=1), then a summary with mean, p50, best, ms per question and q/s), so parse it with regexes and fall back to the raw text when a line does not match. Options are listed in `./laya help`: --preset, --warmup (default 2), --runs (default 5) and --max-batch.
- **Saved results:** store each bench with timestamp, family, device and server flags, so a change (another quant, a driver update) can be compared over time.

### 4.8 Extras

Run history with replay *(implemented: last 50 runs persisted under the user config dir, `H` opens the list, replay re-sends the stored request and is recorded as a new entry)*; A/B compare across two targets (family vs family, or quant vs quant on two ports) *(implemented: `A` opens the screen, each target has a server URL and an optional pinned family, both calls run concurrently, the view lists answer disagreements and both calls enter history)*; threshold rules that map answers to actions; export as `curl`, Python SDK snippet or Jev schema; live-as-you-type mode with a small ad-hoc question set (debounce, cancel in-flight); non-interactive mode (`echo text | playa --preset guard --json`); read-only model inspector (checkpoint, max\_len, max\_batch, size buckets, prompt template, baked-in temperatures) via `laya info`, `laya detect-lang`, `laya list-presets`.

## 5. Go types

```go
type Question struct {
    Type         string          `json:"type"`
    Instructions string          `json:"instructions"`
    Criteria     json.RawMessage `json:"criteria,omitempty"` // object (choice/noul) or array (score)
}

type Preset struct {
    Name, Title, Blurb string
    StateKey           string              `json:"state_key"`
    State              map[string]any      `json:"state"`
    Questions          map[string]Question `json:"questions"` // order lost; keep a key slice
}

type Answer struct {
    Type          string             `json:"type"`
    Confidence    float64            `json:"confidence"`
    Action        *struct{ ActProbability float64 `json:"act_probability"` } `json:"action,omitempty"`
    Noul          *float64           `json:"noul,omitempty"`
    Choice        string             `json:"choice,omitempty"`
    Score         *float64           `json:"score,omitempty"`
    Legend        map[string]string  `json:"legend,omitempty"`
    Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

type Usage struct {
    InputTokens  int     `json:"input_tokens"`
    OutputTokens int     `json:"output_tokens"`
    LatencyMs    float64 `json:"latency_ms"`
}
```

Compute display certainty as 1 - H/ln(n) from the probabilities (use \[p, 1-p\] for noul). Sort score levels numerically, not as strings.

## 6. Build order

1. Go client plus `/health` and one noul decide, printed plainly.
2. Bubble Tea shell, async `tea.Cmd`, telemetry bar, warm-up call.
3. Preset browser and Decision Lens, starting with `guard` (all noul plus one score and one choice).
4. State Canvas with the length guard. *(implemented: text/fields/raw tabs, live guard, model pin)*
5. Question Forge and the JSON toggle. *(implemented: choice/score/noul forms, raw JSON, live preview, save/load)*
6. History *(implemented: bounded store, persisted history screen, exact-request replay)*, batch runner (4.6) *(implemented: file input, chunked requests, sortable/filterable table, exports)*, benchmark pane (4.7) *(implemented: cost readout, warm-run stats, two-result comparison, persisted results, rolling latency fit)*, A/B compare (4.8) *(implemented: two concurrent targets, model and URL switching, disagreement view)*.
7. Process management (spawn) *(implemented: owned `laya serve` child, attach mode, process-group stop, health polling, log tail, warm-all; `laya daemon` and multi-instance spawn remain)*.
8. Snippet library and windowed mode *(implemented: saved states with their key and preset, load into any draft, raw/delete flow; overlapping token windows run sequentially with a strongest-window marker and history recording)*.

Fixtures were consolidated into `internal/api/testdata/`; the old root
`fixtures/` directory is gone. Drafts are per preset and runs use the draft,
so editing never mutates the server copy.

## 7. Open questions

- What `action.act_probability` represents (1 in the built-in presets, 0 in `noul_kev.json`).
- Error shape for an invalid question `type` and for a missing `questions` field.
- Kev's truncation cap is confirmed at 2048 by laya info; still unknown whether its answers differ in shape.
- Whether the question text ever gets truncated (it appears to survive, unproven).
- Confirm lazy loading in the server log.
- Cause of slow first calls after idle.
- `/v1/systemone` matched /v1/decide for one noul request; its score and choice responses and the batch endpoint are untested.

* Whether `--max-batch` can be raised above the GGUF's 8, and whether a 9+ question request then runs as one forward.
* Whether latency steps at the size-bucket boundaries listed by `laya info` (english: 64, 128, 256, 512; Kev: 64 to 2048).
* What happens when a question's instructions plus options exceed `head_max_len` (192 tokens for english): truncated, or an error.
* Whether the multilingual and typed-decisions models have different temperature tables (`laya info <gguf>`); only english and Kev were inspected.
