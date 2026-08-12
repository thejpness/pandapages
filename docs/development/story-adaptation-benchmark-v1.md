# Story adaptation benchmark v1

Identifier: `panda-pages-story-benchmark-v1`

This benchmark evaluates Panda Pages adaptation generation and semantic validation without creating publication approval, publication tickets, or source-eligibility decisions.

## Current suites

### Controlled semantic suite

The committed synthetic fixture corpus under `apps/api/internal/storybenchmark/testdata/controlled` is publication-ineligible test material. Each case defines required and forbidden PR91 semantic findings. Generated-edition fixture Markdown must pass PR91 deterministic validation before it can enter semantic evaluation.

The live Stage 4 CLI evaluates the controlled corpus only. It does not perform live story generation and does not accept an arbitrary source path. End-to-end generation remains available in the internal benchmark runner and will be exposed only after a separately reviewed eligible source fixture is added.

## Technical status versus model quality

`complete` means the benchmark trial completed its technical contract. `incomplete` means transport, decoding, evidence, artifact, binding, or another execution boundary failed.

A semantic `fail` result is model output and must never be used to represent a technical execution error. Incomplete trials are excluded from quality scoring.

## Live execution guard

Live execution is intentionally opt-in and paid. The command requires all of the following:

1. the `--live` flag;
2. `PP_ALLOW_LIVE_STORY_BENCHMARK=1`;
3. `OPENAI_API_KEY` in the environment;
4. no `CI` environment variable.

The API key is not accepted as a command-line argument and is not written to benchmark output.

Run from `apps/api`:

```bash
export OPENAI_API_KEY='...'
export PP_ALLOW_LIVE_STORY_BENCHMARK=1

go run ./cmd/storybenchmark --live
```

The safe default is one validation repetition across the current validator matrix. Repetitions and the matrix can be changed explicitly, for example:

```bash
go run ./cmd/storybenchmark \
  --live \
  --validation-repetitions=3 \
  --models=gpt-5.6-luna,gpt-5.6-terra,gpt-5.6-sol \
  --reasoning-effort=medium \
  --max-output-tokens=8192
```

There are no automatic benchmark retries. A failed or incomplete call remains visible as an incomplete trial.

## Output

Each live run creates a new private local directory beneath:

```text
tmp/storybenchmark/controlled-<UTC timestamp>-<nanoseconds>/
```

The repository already ignores `apps/api/tmp/`.

Each run writes:

- `result.json`: machine-readable benchmark result, model provenance, assessments, scores, and raw token telemetry;
- `report.md`: human-readable summary derived from the result document.

Files are created with mode `0600`; the run directory is created with mode `0700`. Existing run directories are never overwritten.

Token reporting records the existing Responses telemetry: input, cached input, output, reasoning, and total tokens. Stage 4 does not estimate dollar cost because the current transport does not expose cache-write token telemetry and pricing can change independently of the benchmark contract.

## Interpretation boundary

A semantic benchmark pass means an output is suitable to progress to human editorial review under the benchmark expectations. It is not automatic publication approval, a legal conclusion, or permission to bypass human review.
