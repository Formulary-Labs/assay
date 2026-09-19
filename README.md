# assay

Every control gets tested. Every response is validated against 7 criteria before it's accepted. If a run stops, it resumes exactly where it left off.

```bash
go get github.com/Formulary-Labs/assay
```

## What it does

`assay` reads a gemara `ControlCatalog` and a directory of product documentation, then works through each control in batches. For each batch, it validates responses against 7 criteria before accepting them. When a run is interrupted, it resumes from the last saved checkpoint — no work is lost. When all batches are complete, it writes three output artifacts.

## Input

`assay` requires two inputs:

**A gemara ControlCatalog** (`catalog.yaml`) — the framework document that defines the controls being assessed. Validate it with `probe` before passing it to `assay`.

**A product documentation directory** — the source materials `assay` reads to find citations for each control. Can be a directory of Markdown, PDF, HTML, or text files.

## Usage

```go
import "github.com/Formulary-Labs/assay/assessment"

cfg := assessment.Config{
    RunID:          "2026-Q3-run1",
    Program:        "my-program",
    Framework:      "iso27001",
    ProductName:    "My Product",
    ProductVersion: "2.1.0",
    CatalogPath:    "catalog.yaml",
    ProductSource:  "docs/",
    BatchSize:      15,        // controls per batch; default is 15
    OutputDir:      "output/",
}

state, err := assessment.Init(cfg)
```

`Init` extracts the control inventory from the catalog and builds the batch plan. Controls with `AssessmentRequirements` are expanded into sub-entries — each requirement becomes an independent assessment item.

## Batch validation

Before any batch of responses is accepted, each response is validated against these 7 criteria:

| # | Criterion | What fails |
|---|---|---|
| 1 | Citation present | No citation, or citation is generic ("the documentation states...") |
| 2 | Citation quality applied | No quality classification on each citation |
| 3 | Inference boundary respected | `may`/`could` language paired with `satisfied` determination |
| 4 | Coverage completeness | Empty narrative |
| 5 | Narrative addresses the requirement | Fewer than 10 words, or off-topic |
| 6 | Determination explicit and consistent | `architectural_description` citation quality paired with `satisfied` |
| 7 | Gap noted where applicable | `partially_satisfied` or `not_satisfied` with no `gap` field |

Controls that fail validation twice are marked `flagged` for manual review rather than retried indefinitely.

### Citation quality values

| Value | Meaning |
|---|---|
| `direct_assertion` | The source directly states the control requirement is met |
| `topical_reference` | The source covers the topic but does not assert compliance |
| `adjacent_capability` | Related capability — does not directly address the requirement |
| `architectural_description` | Describes architecture only; compliance not asserted |
| `citation_not_found` | No usable citation located |

These values are the vocabulary for evaluating source quality. `direct_assertion` and `topical_reference` are the only values that support a `satisfied` determination without additional investigation.

## State model

Checkpoint state is written to `data/[program]/assessments/[RUN_ID]-state.json` after each batch. If the run is interrupted, call `Init` with the same `RunID` — `assay` detects the existing checkpoint and resumes from the last completed batch.

```json
{
  "run_id": "2026-Q3-run1",
  "program": "my-program",
  "framework": "iso27001",
  "status": "in_progress",
  "batches": [...],
  "controls": {
    "A.5.1": {
      "status": "complete",
      "response": {
        "citation": "Security Policy document, Section 4.2",
        "citation_quality": "direct_assertion",
        "narrative": "...",
        "determination": "satisfied",
        "gap": "",
        "remediation_path": ""
      }
    }
  }
}
```

`status` values: `in_progress`, `complete`, `flagged`, `pending`.

## Output

When all batches are complete, `Assemble()` writes three files to `output_dir`:

| File | Format | Contents |
|---|---|---|
| `[product]-[framework]-assessment.md` | Markdown | Full assessment — summary table and per-control responses with citations |
| `[product]-[framework]-gaps.md` | Markdown | Gap report — all `partially_satisfied`, `not_satisfied`, and `citation_not_found` controls |
| `[product]-[framework]-assessment.json` | JSON | Full state; gemara Layer 5 `EvaluationLog` compatible |

The JSON output is valid input for `challenge` (interrogation) and `titer` (coverage overlay via SOA CSV).

## Pipeline sequence

```
probe catalog.yaml          # validate the catalog first
assay --catalog catalog.yaml --product-source docs/
  → assessment.json         # feed to challenge for interrogation
  → gaps.md                 # feed to specimen to create risk entries
  → assessment.md           # attach to audit package
```

See [CONTRIBUTING.md](https://github.com/Formulary-Labs/.github/blob/main/CONTRIBUTING.md) to contribute.

## License

Apache License 2.0
