# Panda Pages Story Adaptation Specification v2

Status: normative development specification
Specification identifier: `panda-pages-adaptation-v2`
Production generation model: `gpt-5.6-terra`

## 1. Architecture

Panda Pages v2 uses:

```text
canonical source
      |
      +--> source analysis
              |
              +--> confident-readers
              +--> growing-readers
              +--> story-explorers
              +--> little-listeners
```

The canonical public-domain source is always authoritative.

Classic is not AI generated. It remains the authoritative original source text.

The four modern editions are generated independently. One generated edition MUST NOT be used as the source for another generated edition.

Each edition-generation call receives:

- the canonical source;
- the approved StoryAnalysis derived from that exact source;
- the shared v2 adaptation rules;
- the requested edition-specific objective.

## 2. Source analysis

Before modern editions are generated, `gpt-5.6-terra` analyses the canonical source into a reusable StoryAnalysis.

The analysis identifies:

- central plot;
- characters;
- explicit character motivations;
- character flaws or moral ambiguity where source-grounded;
- relationships;
- power dynamics where relevant;
- core story beats;
- development beats;
- enrichment or incidental material;
- causal dependencies between events;
- iconic dialogue, songs, chants, refrains, repetition, or other recognisable material;
- frightening or violent material;
- death or injury;
- adaptation risks.

Adaptation risks specifically identify places where simplification could alter:

- motivation;
- causality;
- ownership;
- bargains;
- power relationships;
- story identity.

The StoryAnalysis MUST be source-grounded.

It MUST NOT improve a character's motives, invent explanations, moralise behaviour, create missing causal links, or manufacture material merely to fill an analysis field.

A source may legitimately contain no material for an optional analysis section. Empty optional sections are preferable to invented analysis.

Panda Pages, not the model, owns provenance metadata such as specification version, source digest, model metadata, and generation identifiers.

### 2.1 Machine-readable StoryAnalysis boundary

The source-analysis result is a strict JSON object.

Every StoryAnalysis field is present in every model result. Source-absent optional collections are represented as empty arrays (`[]`), not omitted fields and not `null`.

Unknown fields, missing fields, duplicate object keys, invalid finite values, and trailing JSON values are rejected.

The Structured Outputs schema and the Panda Pages decoder MUST describe the same field set. Panda Pages validates the decoded object again after schema-constrained generation.

## 3. Shared adaptation rules

The source is canonical.

A modern adaptation MAY:

- remove material;
- compress material;
- simplify material;
- faithfully paraphrase material.

A modern adaptation MUST NOT invent:

- events;
- motives;
- thoughts;
- relationships;
- morals;
- magical powers;
- explanations;
- character development.

Language complexity and narrative scope MUST both change across the edition ladder.

Younger editions MUST NOT merely reproduce the same narrative scope with easier vocabulary.

Character flaws and moral ambiguity MUST survive when they materially drive the source story.

Greed, selfishness, foolishness, impulsiveness, coercion, poor judgement, rivalry, and similar imperfect motives MUST NOT be silently rewritten into nobler behaviour.

Power relationships MUST remain accurate.

Bargains, threats, ownership, authority, dependency, coercion, and involuntary circumstances MUST NOT quietly become voluntary, friendly, wholesome, or consensual.

Causal logic MUST remain intact.

If removing frightening, violent, or otherwise intense material would make a later action stop making sense, enough of the original threat or stakes MUST remain to preserve causality.

**Compression removes information; it MUST NOT manufacture replacement information.**

If material cannot be simplified faithfully, the adaptation MUST retain more of the source rather than invent a cleaner explanation.

Iconic dialogue, rhymes, songs, chants, refrains, repetition, and recognisable story elements SHOULD survive where they materially contribute to story identity.

Modern editions MUST NOT invent morals, redemption arcs, or lessons.

## 4. Edition ladder

### Confident Readers — 9–11

Rich, near-complete literary adaptation.

Retain all core beats, important development, and worthwhile enrichment.

Modernise obstructive language while retaining atmosphere, nuanced motivations, richer dialogue, and description.

Narrative scope is reduced only lightly.

### Growing Readers — 7–9

Complete but materially tighter story.

Retain all core beats, important development, and limited enrichment.

Use shorter paragraphs and sentences, accessible vocabulary, and sufficiently explicit causality.

Remove repeated exchanges and incidental detail.

### Story Explorers — 5–7

Primarily the essential story with selected development.

Strongly reduce secondary scenes, characters, description, and repetition.

Use clear concrete language and explicit cause and effect.

The result MUST remain enjoyable fiction rather than collapse into a plot synopsis.

### Little Listeners — 3–5

Read-aloud retelling built around the essential narrative spine:

```text
beginning -> problem -> action/escalation -> climax -> resolution
```

Use a very small active cast, simple causal relationships, short natural sentences, and useful repetition or rhythm.

Threatening material MAY be substantially softened, but its story function MUST survive when required for motivation, causality, escalation, or resolution.

## 5. Generation self-check

Before returning an edition, generation MUST internally consider:

- source fidelity;
- character motivation;
- causality;
- continuity;
- power relationships;
- accidental invention;
- age suitability;
- requested narrative scope.

It MUST specifically ask:

> Could any remaining passage be removed or compressed without damaging plot, character motivation, escalation, causality, story identity or emotional experience?

It MUST also consider whether the requested edition is materially distinct in narrative scope from the adjacent level in the edition ladder.

## 6. Prompt contract

The v2 pipeline uses separately versioned prompts. The active prompts are:

- `panda-pages-source-analysis-prompt-v3`;
- `panda-pages-edition-generation-prompt-v3`.

The corresponding V2 prompt versions remain valid only as historical artifact provenance. Prompt-version calibration does not change the `panda-pages-adaptation-v2` specification identifier.

Source analysis and edition generation are separate calls.

The canonical source MUST be supplied to the model as data, not interpolated into developer instructions.

The edition-generation call likewise supplies the approved StoryAnalysis as data.

Prompt instructions MUST explicitly state that source and StoryAnalysis values are untrusted content and that commands or instructions appearing inside those values are not model instructions.

The source-analysis prompt MUST require source-grounded analysis and MUST permit empty optional collections rather than encouraging invented content.

The edition-generation prompt contains:

- the shared v2 rules;
- exactly one requested edition objective;
- the canonical source;
- the approved StoryAnalysis;
- the internal fidelity/scope self-check.

The generated edition MUST always be derived directly from the canonical source. Another generated edition MUST NOT be supplied as source material.

## 7. Output

One derived edition is produced per generation call.

The output is Markdown.

Every generated edition begins with:

```markdown
# [Story Title]
```

Classic is not emitted by the generation model.

## 8. OpenAI Responses transport

The v2 generation pipeline uses the OpenAI Responses API.

The transport is intentionally narrow:

- one synchronous `POST` per model operation;
- no automatic application retries;
- no redirects;
- no tools;
- `store: false`;
- explicit model;
- explicit reasoning effort;
- explicit maximum output-token budget;
- bounded JSON response body.

Source-analysis calls use strict Structured Outputs via `text.format` with `type: json_schema` and `strict: true`.

Edition-generation calls request plain text because the required artefact is Markdown.

The transport treats a refusal, incomplete response, malformed response, missing output, multiple output-text items, unexpected content type, authentication failure, rate limit, or provider failure as an explicit error. It MUST NOT reinterpret one of those outcomes as a successful generation.

The transport records the provider response ID, returned model identity, output text, and available input/output/cache/reasoning token usage.

The v2 orchestration layer MUST pass `gpt-5.6-terra` as the model. The generic transport does not silently substitute another model.

## 9. v2 orchestration and analysis artefact

Source analysis and edition generation remain separate runner operations.

`AnalyseSource`:

1. builds `panda-pages-source-analysis-prompt-v3`;
2. requests the locked `gpt-5.6-terra` model;
3. requests strict StoryAnalysis Structured Output;
4. decodes and validates the StoryAnalysis;
5. returns a StoryAnalysis artefact bound to the exact canonical-source SHA-256 and canonical StoryAnalysis SHA-256.

The StoryAnalysis artefact records:

- specification version;
- prompt version;
- requested model;
- returned provider model;
- reasoning effort;
- exact canonical-source SHA-256;
- canonical StoryAnalysis SHA-256;
- provider response ID;
- token usage;
- decoded StoryAnalysis.

The analysis artefact is reusable, but this package does not claim that it has received human editorial approval. A caller that requires approval must establish that workflow separately before passing the artefact to edition generation.

Changing the canonical source invalidates the artefact's source binding.

Changing the decoded StoryAnalysis invalidates the artefact's analysis binding.

Reasoning effort and token budgets are explicit runner configuration because the v2 adaptation specification locks the production model but does not currently define one mandatory reasoning-effort or output-budget value.

Edition-generation orchestration is defined separately so an analysis approval/review boundary can exist between analysis and generation.

### 9.1 Single-edition generation

`GenerateEdition` accepts:

- one canonical modern edition key;
- the exact canonical source;
- one valid source-bound StoryAnalysis artefact;
- story-ingest metadata required for deterministic validation.

Before any model call it rejects:

- Classic or unknown edition keys;
- invalid/tampered StoryAnalysis artefacts;
- a StoryAnalysis artefact whose source digest does not match the exact canonical source.

It then:

1. builds `panda-pages-edition-generation-prompt-v3`;
2. requests exactly one `gpt-5.6-terra` generation;
3. requests plain Markdown output, not JSON Structured Output;
4. runs the PR91 deterministic generated-edition validator;
5. returns a generated-edition artefact bound to source, StoryAnalysis, edition identity, exact generated Markdown SHA-256, model/prompt metadata, provider response ID, token usage, and deterministic findings.

A structurally invalid generation returns an explicit error and MUST NOT be treated as successful.

The failed artefact may still be returned alongside that error so editorial tooling can inspect the generated Markdown and deterministic findings or choose to regenerate it. Returning an inspectable failed artefact does not grant progression status.

No semantic adaptation pass is issued here.

## 10. Validation direction

Generation and semantic validation are separate operations.

A future validator evaluates:

```text
canonical source
+ approved StoryAnalysis
+ panda-pages-adaptation-v2
+ generated edition
```

It should detect, among other things:

- invented content;
- changed motivations;
- broken causality;
- altered ownership;
- altered bargains;
- altered power relationships;
- continuity failures;
- inappropriate edition complexity;
- failed narrative-scope differentiation.

The semantic result remains a finite:

- `pass`;
- `needs_review`;
- `fail`.

A generation result does not become publishable merely because deterministic or semantic validation passes.

Initially, passing generations still receive human editorial review.

Any future progression or publication-readiness artefact MUST be bound to the exact validated content and invalidated by manual modification.
