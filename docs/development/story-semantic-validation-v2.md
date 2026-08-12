# Panda Pages Semantic Adaptation Validation v2

Status: normative foundation for PR93.

This document defines the semantic-assessment artefact used to evaluate Panda Pages v2 generated editions. It does not define publication approval, persistence, or UI behaviour.

## 1. Inputs

A semantic validator evaluates generated content against:

- the exact canonical source;
- the source-bound StoryAnalysis produced under Panda Pages Story Adaptation Specification v2;
- the v2 adaptation specification;
- one generated edition, or a canonical ordered set of modern editions when evaluating progression.

The validator does not replace deterministic validation. PR91 structural validation remains a prerequisite and owns structural findings.

## 2. Result vocabulary

The machine result vocabulary remains the PR91 contract:

- `pass`;
- `needs_review`;
- `fail`.

These correspond to the product concepts PASS, REVIEW_REQUIRED, and FAIL.

PR93 does not rename the PR91 values because they are already established machine-contract vocabulary.

Result invariants remain inherited from PR91:

- `pass` contains no findings;
- `needs_review` contains at least one review finding and no blocking finding;
- `fail` contains at least one blocking finding.

## 3. Finding vocabulary and severity

PR93 reuses the PR91 semantic finding codes and their canonical severities.

It MUST NOT duplicate, reinterpret, or silently widen the finding taxonomy.

Structural finding codes are rejected from semantic assessments.

Edition-only and bundle-only finding rules remain inherited from the PR91 assessment contract.

## 4. Evidence

Every semantic finding MUST contain evidence.

Evidence contains:

- a location;
- an optional edition key where the location is a generated edition;
- an excerpt;
- an explanation of why the excerpt supports the finding.

Supported evidence locations are:

- `canonical_source`;
- `story_analysis`;
- `generated_edition`.

Canonical-source and StoryAnalysis evidence MUST NOT contain an edition key.

Generated-edition evidence MUST identify an edition key that belongs to the assessment target.

Evidence is issue evidence, not a chain-of-thought record. The validator must provide concise externally inspectable support for its finding, not private reasoning.

A `pass` assessment has no findings and therefore no evidence items.

### 4.1 Machine JSON boundary

Semantic-assessment output is a strict JSON object.

Edition and bundle assessments have separate top-level Structured Outputs schemas because their target fields differ.

Every finding contains an `evidence` array.

Every evidence object has one stable four-field machine shape:

- `location`;
- `editionKey`;
- `excerpt`;
- `explanation`.

For `canonical_source` and `story_analysis` evidence, `editionKey` is explicitly `null`.

For `generated_edition` evidence, `editionKey` is a concrete canonical modern edition key.

Unknown fields, missing fields, duplicate object keys, null arrays, invalid versions, invalid target shapes, unsupported finding codes, wrong severities, structural finding codes, and trailing JSON values are rejected.

The JSON Schema intentionally does not duplicate PR91's semantic finding-code enum or canonical severity mapping. Those remain owned by the PR91 adaptation contract and are applied again after decoding.

## 5. Assessment scopes

### Edition

An edition assessment evaluates exactly one canonical modern edition.

It can detect source-fidelity, motivation, causality, continuity, power-relationship, invention, age/scope, vocabulary, intensity, iconic-language, and related v2 problems represented by the PR91 edition finding vocabulary.

### Bundle

A bundle assessment evaluates two or more modern editions in canonical edition order.

Its purpose is progression comparison, including whether adjacent levels are materially distinct in narrative scope and whether progression is inverted.

Bundle assessment is not a substitute for per-edition semantic assessment.

## 6. Contract versions

The semantic-validation artefact version is:

`panda-pages-semantic-validation-v2`

The adaptation specification version is independently bound as:

`panda-pages-adaptation-v2`

Keeping these identifiers separate allows the semantic validator contract to evolve without silently reinterpreting the adaptation-generation specification.

## 7. Prompt contract

PR93 defines two separately versioned semantic-validator prompts:

- `panda-pages-edition-validation-prompt-v2`;
- `panda-pages-bundle-validation-prompt-v2`.

Both prompts receive model inputs as JSON data rather than interpolating source or generated story text into developer instructions.

The developer instructions explicitly treat canonical source, StoryAnalysis, generated Markdown, and strings inside those values as untrusted content.

Before a prompt can be built:

- the StoryAnalysis artefact must validate and match the exact canonical source;
- every generated-edition artefact must validate;
- every generated edition must already pass PR91 deterministic validation;
- source and StoryAnalysis digest bindings must match;
- bundle targets must contain at least two distinct editions in canonical modern edition order.

### 7.1 Edition prompt

The edition validator evaluates one generated modern edition against the canonical source, StoryAnalysis, and requested level.

It may emit only PR91 semantic edition findings.

It explicitly checks fidelity, motivations, causal logic, continuity, power relationships, invention, story identity, iconic material, age-level narrative scope, language complexity, content intensity, connective material, and softened lethal or frightening outcomes.

### 7.2 Bundle prompt

The bundle validator evaluates progression across two or more canonical ordered modern editions.

It is not a substitute for per-edition semantic assessment.

It may emit only PR91 bundle findings and focuses on meaningful adjacent-level narrative-scope progression, inverted progression, and questionable/weak differentiation.

### 7.3 Evidence instructions

Prompt output must use concise externally inspectable evidence, preferably exact or tightly bounded excerpts from supplied data.

Evidence explanations support findings only; they are not chain-of-thought records and must not contain hidden model reasoning.

## 8. Semantic-validator runner

The semantic-validator model is explicit runner configuration.

Panda Pages v2 currently locks `gpt-5.6-terra` for generation only. PR93 does not silently assume that the same model is the correct long-term semantic assessor before validator benchmarking has established that choice.

The runner config therefore requires:

- a Responses gateway;
- an explicit validator model;
- an explicit reasoning effort;
- an explicit output-token budget.

Edition and bundle validation each perform exactly one Structured Outputs model call using their corresponding strict schema.

After decoding, the runner verifies that the returned assessment target exactly matches the requested edition or canonical ordered bundle.

### 8.1 Evidence verification

Model-supplied evidence is verified before an assessment artefact is accepted.

A `canonical_source` excerpt must occur in the exact supplied canonical source.

A `story_analysis` excerpt must occur in one concrete StoryAnalysis string field.

A `generated_edition` excerpt must occur in the exact Markdown for the referenced generated edition.

Fabricated, paraphrased-as-if-quoted, or target-mismatched evidence therefore fails closed instead of being accepted as support for a finding.

### 8.2 Bound assessment artefact

A successful semantic-validator call produces an artefact bound to:

- semantic-validation version;
- adaptation-specification version;
- prompt version;
- assessment scope and exact edition target(s);
- requested and returned validator model;
- reasoning effort;
- canonical-source SHA-256;
- StoryAnalysis SHA-256;
- each generated-edition content SHA-256;
- canonical assessment SHA-256;
- provider response ID;
- token usage;
- the decoded evidence-bearing assessment.

Changing the assessment invalidates its assessment digest.

Changing source, StoryAnalysis, or generated Markdown invalidates the corresponding upstream binding and therefore prevents the same artefact from certifying modified content.

No publication or progression ticket is issued by this runner.

## 9. Human review and publication

A semantic `pass` in PR93 means only that the evaluated artefact satisfies the semantic validator contract sufficiently to progress to human editorial review.

It does not mean:

- publication approved;
- copyright or legal clearance granted;
- universally suitable for every child;
- publication certification issued.

Initially, passing generations still require human review.

Automated progression/publication tickets are explicitly out of scope for PR93 and must be introduced only after benchmark evidence demonstrates reliable agreement with human judgement.

Any future ticket must bind the exact source, StoryAnalysis, generated content, validator contract, and validator result so manual edits invalidate prior certification.
