# Panda Pages Story Adaptation Contract v1

Status: normative development contract
Contract identifier: `panda-pages-adaptation-v1`

## 1. Purpose

This contract defines what it means for a Panda Pages story edition to be an acceptable adaptation of an authoritative source story.

It converts the settled principles from **Panda Pages Story Generation Master Prompt v1.4** into a versioned product contract that can later be enforced by deterministic validation, semantic assessment, editorial review, and generation tooling.

A result of `pass` under this contract means only:

> the edition satisfies the Panda Pages adaptation contract closely enough to progress to the next editorial workflow stage.

It does **not** mean:

- the source is legally eligible for Panda Pages;
- the edition is automatically publishable;
- the edition is suitable for every child;
- the edition is factually or editorially perfect;
- the edition has passed source-quality, release, or publication checks.

Source eligibility remains a separate upstream boundary. Release and publication remain separate downstream actions.

### Normative language

The uppercase terms `MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, and `MAY` are normative contract language.

- `MUST` / `MUST NOT` define requirements whose violation is blocking unless this contract explicitly says otherwise.
- `SHOULD` / `SHOULD NOT` define strong expectations whose unresolved violation normally requires review.
- `MAY` defines an allowed choice, not a requirement.

## 2. Authoritative source and derivation model

The authoritative source story is the canonical narrative reference.

The five Panda Pages editions are:

1. `classic`
2. `confident-readers`
3. `growing-readers`
4. `story-explorers`
5. `little-listeners`

Derived editions MUST be adapted from the authoritative source story, not from another simplified edition.

The derivation model is:

```text
authoritative canonical source
        |
        +--> classic (source-preserving projection; no semantic rewrite)
        |
        +--> confident-readers
        +--> growing-readers
        +--> story-explorers
        +--> little-listeners
```

Classic is not an LLM adaptation target. Generation tooling SHOULD take the canonical Classic/source as input and generate only the four modern derived editions.

The four modern editions may deliberately reduce language complexity and narrative scope. They must still remain recognisably the same story.

## 3. Edition intent

### 3.1 Classic

Approximate audience: 11+.

Classic is the fullest source-preserving reading experience.

Classic MUST preserve the complete historical source text and narrative content except for purely mechanical extraction, boilerplate removal, formatting, or ingestion normalisation already required to establish the canonical Panda Pages source.

Classic MUST NOT be semantically rewritten by generation tooling.

Classic MUST NOT silently modernise plot, violence, relationships, historical attitudes, vocabulary, or outcomes merely to match younger-edition rules.

### 3.2 Confident Readers

Approximate audience: 9–11.

Confident Readers is the richest modern adaptation.

It SHOULD retain most meaningful scenes, character detail, motivations, relationships, atmosphere, dialogue, and plot texture while modernising language where useful.

It MAY streamline secondary material, repetition, long description, and archaic construction.

### 3.3 Growing Readers

Approximate audience: 7–9.

Growing Readers is a substantial but deliberately reduced adaptation.

It SHOULD preserve the full central story while reducing secondary episodes, descriptive density, syntactic complexity, and non-essential historical detail.

### 3.4 Story Explorers

Approximate audience: 5–7.

Story Explorers focuses on the central narrative journey.

It SHOULD keep the essential characters, motivations, conflict, major turning points, climax, and resolution while removing or compressing secondary material that is not required to understand the story.

### 3.5 Little Listeners

Approximate audience: 3–5.

Little Listeners is the most compact read-aloud adaptation.

It MUST preserve the recognisable heart of the story: who matters, what they want or fear, what goes wrong, the essential consequence, and how the story resolves.

It SHOULD use the clearest narrative line, concrete language, short causal steps, and read-aloud-friendly phrasing.

## 4. Internal story map

Before adapting the source, generation or assessment SHOULD identify an internal story map.

The story map consists of:

- central premise;
- initial state;
- main characters;
- important relationships;
- character motivations;
- character flaws or significant imperfections;
- central conflict;
- essential stakes;
- major turning points;
- climax;
- resolution;
- major outcomes;
- plot-significant locations or objects;
- valuable iconic language, refrains, rhymes, chants, songs, or repeated dialogue;
- source events whose removal would create later continuity problems.

The story map is an analysis tool. It is not reader-facing output.

## 5. Core story invariants

Every derived edition MUST preserve the story's identity.

The following are blocking invariants unless an explicitly documented adaptation rule says otherwise:

### 5.1 Central plot

The edition MUST preserve the central plot and major outcomes.

A shorter edition may remove secondary episodes. It MUST NOT replace the central story with a different adventure.

### 5.2 Beginning, conflict, climax, and resolution

The edition MUST preserve the meaningful beginning state, central conflict, climax, and resolution.

Compression is allowed. Contradictory replacement is not.

### 5.3 Main characters

Main characters MUST remain recognisably the same characters.

An edition MUST NOT silently introduce a new major character to repair a simplification, merge major characters in a way that changes the story, or remove a main character whose role is necessary to the central plot.

### 5.4 Relationships

Important relationships MUST retain their narrative meaning.

Family, friendship, rivalry, coercion, authority, dependency, trust, betrayal, and romantic relationships MUST NOT be reversed or materially reframed merely to make the story simpler.

### 5.5 Motivation and imperfection

Significant motivations and character imperfections MUST survive when they drive the plot.

Greed may remain greed. Curiosity may remain curiosity. Pride, jealousy, disobedience, impatience, selfishness, fear, poor judgement, and rivalry may remain when they matter.

Derived editions MUST NOT convert every character into an idealised model of kindness or insert invented moral dialogue that changes why characters act.

## 6. Causal fidelity

Adaptation MUST preserve understandable causal logic.

The core pattern is:

```text
event -> motivation -> decision -> consequence
```

An edition may shorten explanation, but the reader must still be able to understand why the next important event happens.

Removing a scene is not acceptable when its removal causes:

- an unexplained decision;
- a character appearing somewhere without reason;
- an object appearing without introduction when it is plot-significant;
- a consequence without a cause;
- a threat with no understandable stakes;
- a resolution that no longer follows from the preceding story.

## 7. Narrative-scope reduction

Panda Pages editions are not intended to be the same manuscript with progressively easier vocabulary.

Scope reduction MUST be deliberate.

As editions descend from Confident Readers to Little Listeners, they may progressively reduce:

- secondary episodes;
- repeated encounters;
- descriptive passages;
- exposition;
- side characters;
- minor locations;
- historical digressions;
- extended dialogue;
- subplots;
- narrative framing that does not affect the core story.

Reduction MUST preserve continuity and the recognisable story heart.

A removed scene MUST NOT continue to affect later text as though it still occurred.

## 8. Allowed modernisation

Modern derived editions MAY:

- modernise archaic syntax;
- replace opaque wording with clearer modern wording;
- shorten sentences;
- shorten dialogue;
- reduce descriptive density;
- explain necessary uncommon terms through context;
- remove incidental adult-coded references that do not affect the plot;
- reduce non-essential graphic or sensory detail;
- compress repetition;
- simplify historical context that is not necessary to understand the story;
- use minor connective wording needed to bridge compressed events.

These permissions do not allow substantial invented plot material.

## 9. Invention boundary

Derived editions MUST NOT invent substantial new narrative material.

Prohibited invention includes:

- replacement adventures;
- new major characters;
- new central conflicts;
- new climaxes;
- new endings;
- invented moral lessons presented as though they belong to the source;
- invented speeches that materially change motivation or relationships;
- invented punishments or rewards that change the source's narrative meaning.

Minor connective material is allowed only when needed to join retained source events coherently.

Connective material MUST remain subordinate to the source story and MUST NOT become a new event of independent narrative significance.

## 10. Violence, cruelty, peril, and lethal outcomes

Adaptation MUST distinguish narrative function from presentation intensity.

### 10.1 Preserve function

If danger, violence, cruelty, capture, punishment, or threat is necessary to explain character motivation, stakes, conflict, climax, or resolution, its narrative function MUST remain understandable.

Younger editions MAY reduce the detail and emotional intensity of how it is presented.

They MUST NOT simply remove the threat when doing so makes the remaining story incoherent.

### 10.2 Graphic detail

Modern editions MAY reduce graphic, sensory, prolonged, or distressing description when that detail is not itself necessary to the story's identity.

### 10.3 Lethal outcomes

For modern children's editions, a lethal outcome MAY be replaced with a coherent non-lethal consequence where the precise death is not essential to the identity or causal resolution of the story.

When an outcome is changed:

- the replacement MUST preserve the narrative consequence;
- later events MUST reflect that the character survived or was removed differently;
- surviving characters MUST NOT silently disappear;
- the edition MUST NOT contradict its own changed continuity;
- the edition MUST NOT keep an inherently cruel lethal mechanism and merely downgrade the final injury.

Classic is source-preserving and is not governed by this modernisation permission.

### 10.4 Gratuitous restoration

A modern edition MUST NOT reintroduce historical graphic violence or cruelty that was deliberately and coherently omitted from that edition's adaptation.

## 11. Coercive and harmful relationships

Simplification MUST NOT romanticise coercion, captivity, abuse, forced dependency, or other harmful relationship dynamics.

An edition may use age-appropriate language to describe the situation.

It MUST NOT transform coercion into affection or consent merely because explicit explanation was removed.

## 12. Historical and adult-coded material

Historical context SHOULD be retained when it:

- affects the plot;
- affects a character's choices;
- explains a relationship or power structure;
- is necessary to understand an object, location, occupation, custom, or consequence;
- contributes materially to the identity of the story.

Incidental historical or adult-coded references MAY be removed from modern editions when they do not materially affect the story.

Retained historical references SHOULD earn their place in the edition rather than survive automatically because they exist in the source.

Classic remains source-preserving.

## 13. Iconic language and repeated material

Source rhymes, chants, songs, repeated dialogue, refrains, catchphrases, and other recognisable language SHOULD be preserved when they are valuable to story identity, rhythm, memory, or cultural recognition.

They MAY be shortened where required by edition scope.

Removing them is not automatically a failure, but unexplained removal of highly story-defining material SHOULD produce editorial review.

## 14. Vocabulary and prose progression

Vocabulary and syntax MUST fit the requested edition.

The contract intentionally does not define universal fixed word-count bands in v1.

### Confident Readers

May retain richer vocabulary, longer sentences, more description, and more historical language when meaning remains clear from context.

### Growing Readers

Should prefer familiar modern vocabulary, moderate sentence length, reduced clause density, and direct explanation of necessary unfamiliar concepts.

### Story Explorers

Should avoid specialist, archaic, or unnecessarily abstract vocabulary. Sentences should normally carry one clear narrative idea at a time.

### Little Listeners

Should use concrete, read-aloud-friendly language and avoid uncommon historical vocabulary unless the word is essential and immediately understandable from context.

Vocabulary simplification MUST NOT erase necessary stakes, motivation, relationships, or causal logic.

## 15. Continuity

Every edition MUST be internally self-consistent.

Blocking continuity failures include:

- references to removed scenes as though they occurred;
- characters knowing information they no longer learned;
- unexplained location changes;
- objects appearing or disappearing inconsistently;
- a changed death/removal that is ignored later;
- a survivor disappearing because the source assumed their death;
- contradictory names, relationships, motives, or outcomes.

## 16. Output structure

Panda Pages recognises these exact edition identities:

```text
classic
confident-readers
growing-readers
story-explorers
little-listeners
```

Automatic adaptation generation takes the canonical Classic/source as input and emits these four modern edition identities:

```text
confident-readers
growing-readers
story-explorers
little-listeners
```

The existing file-ingest workflow may still ingest all five exact filenames:

```text
classic.md
confident-readers.md
growing-readers.md
story-explorers.md
little-listeners.md
```

Generated story content MUST:

- be valid UTF-8;
- be non-empty Markdown;
- use Markdown as the authoring format;
- begin with an H1 story title for generation workflows that emit standalone edition files;
- contain no raw HTML;
- remain compatible with Panda Pages server-side story ingestion.

Relative word count, sentence length, or similar complexity metrics MAY be used as review signals. They MUST NOT by themselves prove semantic fidelity or produce a semantic `pass`.

## 17. Assessment model

Any semantic or combined assessment producer MUST return a finite result:

- `pass`
- `needs_review`
- `fail`

There are two assessment scopes.

### 17.1 Edition assessment

An edition assessment compares one modern derived edition directly with the authoritative source and the requested edition intent.

It is responsible for source fidelity, causal fidelity, character fidelity, continuity, invention boundaries, intensity, vocabulary, and edition-specific narrative scope.

Classic source preservation is enforced deterministically and is not delegated to a semantic assessment.

### 17.2 Bundle assessment

A bundle assessment compares two or more modern editions emitted by the same generation operation.

Bundle edition keys MUST appear in canonical modern-edition order, even when the assessment covers only a subset of the four modern editions.

It is responsible for cross-edition properties that cannot be established reliably from one edition in isolation, including:

- the editions are meaningfully differentiated;
- narrative scope reduces in the intended direction;
- complexity does not invert the edition hierarchy;
- the set is not merely one manuscript with superficial vocabulary substitutions.

A generation operation that emits only one modern edition does not require a bundle assessment.

### 17.3 Result and severity invariants

Finding severities are finite:

- `blocking`
- `review`

Each finding code has the canonical severity class defined by this contract. An assessment is invalid if a finding supplies a different severity.

Result consistency is exact:

- `pass` MUST contain no findings;
- `needs_review` MUST contain at least one `review` finding and no `blocking` findings;
- `fail` MUST contain at least one `blocking` finding and MAY also contain `review` findings.

An internally inconsistent assessment MUST be rejected rather than normalised.

### 17.4 Pass

`pass` means:

- no findings are present;
- required structural checks pass;
- the edition or bundle is judged to satisfy the applicable requirements of this adaptation contract.

A pass may progress to the next editorial workflow stage.

A pass MUST NOT trigger publication by itself.

### 17.5 Needs review

`needs_review` means:

- no definite blocking contradiction has been established;
- at least one material judgement cannot safely be resolved automatically;
- human editorial review is required before progression.

### 17.6 Fail

`fail` means:

- one or more blocking contract violations are established; or
- required structural validation fails.

A failed edition MUST NOT receive a progression ticket.

A failed bundle MUST NOT authorise automatic progression of that generated batch.

## 18. Finding taxonomy

Finding codes are versioned contract data. Unknown codes MUST NOT be silently treated as passing.

### 18.1 Blocking semantic findings

- `core_plot_changed`
- `major_outcome_changed`
- `climax_changed`
- `resolution_changed`
- `main_character_changed`
- `relationship_changed`
- `motivation_changed`
- `causal_chain_broken`
- `stakes_removed`
- `substantial_material_invented`
- `invented_moralising`
- `continuity_error`
- `survivor_continuity_error`
- `coercion_romanticised`
- `edition_identity_lost`
- `edition_progression_not_distinct`
- `edition_progression_inverted`

### 18.2 Review semantic findings

- `scope_too_rich`
- `scope_too_thin`
- `vocabulary_mismatch`
- `content_intensity_mismatch`
- `historical_context_questionable`
- `iconic_language_removed`
- `connective_material_questionable`
- `lethal_outcome_substitution_questionable`
- `edition_progression_questionable`

### 18.3 Structural findings

- `invalid_edition_key`
- `invalid_utf8`
- `empty_markdown`
- `missing_h1_title`
- `raw_html_present`
- `classic_source_changed`
- `ingest_incompatible`

Structural findings are blocking unless a later contract version explicitly reclassifies them.

### 18.4 Finding scope

Semantic finding codes are scoped.

Edition assessments MAY emit the semantic findings in sections 18.1 and 18.2 except:

- `edition_progression_not_distinct`;
- `edition_progression_inverted`;
- `edition_progression_questionable`.

Those three progression findings are bundle-only and MUST NOT appear in an edition assessment.

Bundle assessments MAY emit only:

- `edition_progression_not_distinct`;
- `edition_progression_inverted`;
- `edition_progression_questionable`.

Structural findings are produced by deterministic validation rather than semantic assessment. They MUST NOT be invented by a semantic assessor.

An assessment containing a finding outside the allowed scope for that assessment is invalid.

## 19. Machine-readable assessment envelope

The target edition-assessment shape is conceptually:

```json
{
  "contractVersion": "panda-pages-adaptation-v1",
  "assessmentScope": "edition",
  "editionKey": "story-explorers",
  "result": "needs_review",
  "findings": [
    {
      "code": "lethal_outcome_substitution_questionable",
      "severity": "review",
      "message": "The non-lethal replacement may alter the source resolution."
    }
  ]
}
```

The target bundle-assessment shape is conceptually:

```json
{
  "contractVersion": "panda-pages-adaptation-v1",
  "assessmentScope": "bundle",
  "editionKeys": [
    "confident-readers",
    "growing-readers",
    "story-explorers",
    "little-listeners"
  ],
  "result": "pass",
  "findings": []
}
```

The message is explanatory text and MUST be non-empty when a finding exists.

The machine-readable JSON boundary MUST accept exactly one valid UTF-8 JSON object.

The top-level fields are exact:

- edition assessment: `contractVersion`, `assessmentScope`, `editionKey`, `result`, `findings`;
- bundle assessment: `contractVersion`, `assessmentScope`, `editionKeys`, `result`, `findings`.

`findings` MUST be a JSON array, including an empty array for `pass`.

Unknown fields, missing required fields, duplicate object keys, and trailing JSON values MUST be rejected rather than ignored. Finding objects likewise use only the exact `code`, `severity`, and `message` fields.

The contract version, assessment scope, edition identity, finding code, severity, and final result are finite contract data.

An implementation MUST reject internally inconsistent envelopes, including:

- `pass` with any finding;
- `needs_review` without a review finding;
- `needs_review` with a blocking finding;
- `fail` without a blocking finding;
- a finding whose supplied severity does not match the canonical severity for its code;
- a finding that is not allowed for the assessment scope;
- an edition assessment without exactly one canonical modern edition key;
- a bundle assessment with fewer than two distinct canonical modern edition keys;
- bundle edition keys that are not in canonical modern-edition order;
- unknown contract versions;
- unknown assessment scopes;
- unknown edition keys;
- unknown finding codes;
- unknown severities.

## 20. Progression ticket semantics

A future edition progression ticket may be issued only from a valid `pass` edition assessment.

When multiple modern editions are generated as one automated batch, automatic progression of the complete batch additionally requires a valid `pass` bundle assessment. A human editorial workflow may still handle editions individually.

The ticket means:

> this exact edition content passed `panda-pages-adaptation-v1` and may progress to the next editorial workflow stage.

The ticket MUST be bound to the exact assessed edition content, for example by a content digest.

A bundle assessment MUST likewise be bound to the exact ordered set of assessed edition contents.

Changing an edition's content MUST invalidate its prior edition ticket and any bundle assessment that included that edition.

A ticket MUST NOT claim:

- copyright eligibility;
- legal clearance;
- universal child suitability;
- source-quality approval;
- publication approval.

## 21. Final internal generation checklist

The first 25 checks below preserve the settled Master Prompt v1.4 review criteria recovered from the earlier generation work. Checks 26–30 are PR91 structural additions needed to turn that prompt discipline into an enforceable product contract.

Before an edition set can be considered successfully generated, confirm:

1. All modern editions preserve the shared central plot and major outcomes.
2. Confident Readers retains the richest modern narrative scope.
3. Growing Readers deliberately reduces secondary content.
4. Story Explorers follows the central narrative journey.
5. Little Listeners preserves the recognisable heart of the story.
6. The editions are not merely the same manuscript with easier vocabulary.
7. Removed scenes create no continuity errors.
8. Content intensity is appropriate to each requested edition.
9. Modern editions do not gratuitously restore historical graphic violence.
10. A lethal mechanism is not merely kept intact and relabelled with a softer injury.
11. No invented moral dialogue changes the source story.
12. No substantial invented narrative material is added.
13. Any connective material is minor and subordinate to retained source events.
14. Stakes remain clear enough to explain important motivations and decisions.
15. Coercive or harmful relationships are not romanticised through simplification.
16. Valuable source rhymes, chants, songs, refrains, or repeated dialogue are preserved where appropriate.
17. Changed deaths or removals correctly account for characters who now survive.
18. No survivor silently disappears because the source assumed a different outcome.
19. Survival or removal changes create no later continuity contradiction.
20. Incidental adult-coded references are appropriately filtered from modern editions.
21. Retained historical references earn their place by serving story understanding or identity.
22. Vocabulary fits the requested edition.
23. Little Listeners avoids unnecessary uncommon historical vocabulary.
24. Story Explorers avoids unnecessary specialist or archaic terminology.
25. Confident Readers may retain richer vocabulary where its meaning is clear or useful.
26. Every emitted modern edition uses a canonical Panda Pages modern edition key.
27. Every emitted modern edition is valid non-empty UTF-8 Markdown.
28. Every standalone generated modern edition begins with an H1 story title.
29. Generated content remains compatible with Panda Pages story ingestion and contains no raw HTML.
30. No edition or bundle receives `pass` when any contract finding remains.

## 22. Contract-version rule

`panda-pages-adaptation-v1` is immutable once used to issue stored assessments or progression tickets.

Future policy changes MUST create a new contract version rather than silently changing the meaning of an existing stored pass.
