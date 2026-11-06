# Server behavior verified against `laya serve`

Observed on Vulkan (AMD RX 580, no fp16), laya models from
convaiinnovations/laya* and kev-0.8b. Numbers are from that machine.

## API
- `POST /v1/systemone` (documented) and `/v1/decide` returned the same response for the same noul request.
- `GET /health`, `GET /v1/models` (`{"models":[...]}`, mixes families and aliases), `GET /v1/presets` (bare array, ready to POST).
- Errors: `{"error":{"code","message","param"}}`; `param` is a field path.
- `laya daemon` takes the same body per line, answers with the same response plus an `id`.

## Answers
- noul: `noul` = P(yes); `confidence` = max(p, 1-p).
- choice: `choice` + `probabilities` by key; `confidence` = 1 - normalized entropy.
- score: `score` = expected level index; `legend` + `probabilities` keyed "0".."n-1"; criteria is an ordered array.
- `action.act_probability` was always 1; meaning unknown.

## Limits
- max_len 512 english, 1024 multilingual, 2048 kev; max_opts 16; max_batch 8 (GGUF default, `--max-batch` lowers it).
- english head_max_len 192 (likely the question + options budget).
- Truncation is silent and cuts the END of the state; the question survives.
- `input_tokens` is the sum over questions; a state over the cap gives n_questions x cap.
- Compute follows questions x longest sequence (`pad: max-in-batch`).

## Timing
- Single noul ~60 ms warm; first call after idle 250-650 ms.
- Email preset (7 questions): 359 ms in `laya bench` (1 forward); 503 ms with `--max-batch 4` (2 forwards).
- A 9th question forces a second forward (+140 to +230 ms).
- First Kev call 1.36 s, then ~100 ms (very likely lazy loading).

## Calibration
- Temperatures are baked in and depend on question type and option count (english: choice 11+ options uses 0.10, 3-5 uses 1.76). Compare certainty only between similar questions.

## Open
- Meaning of `act_probability`; error shape for an invalid question type; lazy-loading confirmation; cause of slow first calls after idle; whether `--max-batch` can exceed 8; behavior past head_max_len.
