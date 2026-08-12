# Story adaptation benchmark v1

Identifier: `panda-pages-story-benchmark-v1`

This benchmark evaluates Panda Pages adaptation generation and semantic validation without creating publication approval, publication tickets, or source-eligibility decisions.

## Suites

### Controlled semantic suite

The committed synthetic fixture corpus under `apps/api/internal/storybenchmark/testdata/controlled` is publication-ineligible test material. Each case defines required and forbidden PR91 semantic findings. Generated-edition fixture Markdown must pass PR91 deterministic validation before it can enter semantic evaluation.

The controlled live mode evaluates this fixed corpus only. It performs semantic-validation calls but does not perform story generation and does not accept an arbitrary source path.

### Reviewed end-to-end suite

The end-to-end suite uses exactly one committed source under `apps/api/internal/storybenchmark/testdata/publicdomain/benjamin-bunny`: *The Tale of Benjamin Bunny* by Beatrix Potter, Project Gutenberg ebook `14407`. The fixture is bound to an exact canonical-source SHA-256 and a reviewed evidence snapshot under `panda-pages-copyright-v3`.

Loading the fixture re-runs the deterministic Panda Pages copyright policy and fails closed unless both the US and UK assessments are eligible. The loader also requires the exact Project Gutenberg provider identity and landing URL. The live CLI has no source-file or source-URL flag: end-to-end generation can only use this reviewed fixture.

Generation reuses PR92 directly. Its model remains fixed by the generation contract to `gpt-5.6-terra`; the benchmark records the explicit analysis and edition reasoning efforts and output-token budgets used for the run. Each generation repetition independently performs one StoryAnalysis call and four edition-generation calls. Every validator configuration then evaluates the same exact four generated artifacts for that repetition: four edition assessments plus one bundle assessment per validation repetition.

## Technical status versus model quality

`complete` means the benchmark trial completed its technical contract. `incomplete` means transport, decoding, evidence, artifact, binding, structural validation, or another execution boundary failed.

A semantic `fail` result is model output and must never be used to represent a technical execution error. Incomplete validation trials are excluded from semantic or human-review agreement scoring. There are no automatic benchmark retries.

## Live execution guard

Paid execution is intentionally opt-in. Both live modes require all of the following:

1. the `--live` flag;
2. `PP_ALLOW_LIVE_STORY_BENCHMARK=1`;
3. `OPENAI_API_KEY` in the environment;
4. no `CI` environment variable.

The API key is not accepted as a command-line argument and is not written to benchmark output.

Run from `apps/api`.

Controlled suite:

```bash
export OPENAI_API_KEY='...'
export PP_ALLOW_LIVE_STORY_BENCHMARK=1

go run ./cmd/storybenchmark --live
```

End-to-end suite:

```bash
export OPENAI_API_KEY='...'
export PP_ALLOW_LIVE_STORY_BENCHMARK=1

go run ./cmd/storybenchmark \
  --mode=end-to-end \
  --live \
  --generation-repetitions=1 \
  --validation-repetitions=1 \
  --models=gpt-5.6-luna,gpt-5.6-terra,gpt-5.6-sol \
  --reasoning-effort=medium \
  --max-output-tokens=8192 \
  --analysis-reasoning-effort=medium \
  --analysis-max-output-tokens=16384 \
  --edition-reasoning-effort=medium \
  --edition-max-output-tokens=32768
```

Before making any live request, the end-to-end mode loads the committed fixture, verifies its exact source SHA-256, and re-evaluates its committed evidence through the current copyright policy. The command prints the planned number of paid requests before orchestration begins.

## Output

Each live run creates a new private local directory beneath `apps/api/tmp/storybenchmark/`:

```text
controlled-<UTC timestamp>-<nanoseconds>/
end-to-end-<UTC timestamp>-<nanoseconds>/
```

The repository ignores `apps/api/tmp/`.

Controlled runs write:

- `result.json`: machine-readable benchmark result, model provenance, assessments, scores, and raw token telemetry;
- `report.md`: human-readable summary derived from the result document.

End-to-end runs write:

- `result.json`: exact source/provenance binding, generation configuration, PR92 analysis and generated-edition artifacts, PR93 assessments, response IDs, and token telemetry;
- `report.md`: derived technical and token summary;
- `human-review-template.json`: an editorial-review template bound to the exact StoryAnalysis SHA-256 and generated edition content SHA-256 values.

Files are created with mode `0600`; run directories are created with mode `0700`. Existing run directories and result files are never overwritten.

Live execution wraps the Responses gateway with benchmark-owned telemetry before generation or semantic validation begins. The benchmark records request metadata plus response ID, returned model, and token usage for every successful Responses API result, even when downstream decoding, evidence verification, binding, structural validation, or scoring later marks the benchmark work incomplete. Raw prompts and model output are not duplicated into this telemetry stream. Failed API requests are recorded as failed attempts, but if no `ResponsesResult` exists there is no token usage available to record; provider-side work for such failures therefore cannot be inferred from the benchmark artifact.

The existing `usage` block remains retained-artifact telemetry derived from valid PR92/PR93 artifacts. The separate `responsesApiTelemetry` block is the correct benchmark view for comparing observed API usage across validator configurations because it also includes successful model responses that were subsequently rejected by stricter Panda Pages validation.

## Human-review comparison

Human review is deliberately a second, offline step. It never makes an OpenAI request.

After an end-to-end run, copy `human-review-template.json` to a separate review file. For each target:

1. review the exact generated Markdown identified by its SHA-256 binding;
2. change `reviewStatus` from `pending` to `complete`;
3. set `expectedResult` to `pass`, `needs_review`, or `fail`;
4. list the complete PR91 semantic finding-code set you expect in `expectedFindingCodes` (non-pass results require at least one code);
5. record an editorial note as useful.

Then score that review against the saved benchmark result:

```bash
go run ./cmd/storybenchmark \
  --mode=human-review \
  --result-json=tmp/storybenchmark/end-to-end-.../result.json \
  --human-review=/path/to/completed-human-review.json
```

Human-review mode refuses `--live` and does not require `OPENAI_API_KEY`. It rejects pending targets, malformed taxonomy, wrong assessment scope, missing targets, changed source bindings, changed StoryAnalysis SHA-256 values, or changed generated-edition SHA-256 values. A review therefore cannot silently score regenerated content.

Successful comparison writes, without overwriting existing files:

- `human-review-score.json`;
- `human-review-report.md`.

The comparison reports result agreement and exact finding-set agreement separately, plus full agreement, globally and by validator configuration.

## Interpretation boundary

A semantic benchmark pass means an output is suitable to progress to human editorial review under the benchmark expectations. Human-review agreement is evidence for selecting and configuring a validator; it is not automatic publication approval, a legal conclusion, or permission to bypass human review or source eligibility.
