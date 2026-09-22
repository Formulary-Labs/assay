// Package assessment implements the core assessment logic for assay:
// control inventory extraction from a gemara ControlCatalog, batch planning,
// and the 7-criterion batch validation.
package assessment

import (
	"fmt"
	"strings"
	"time"

	gemara "github.com/gemaraproj/go-gemara"

	"github.com/Formulary-Labs/assay/state"
	"github.com/Formulary-Labs/substrate/artifact"
)

// DefaultBatchSize is the default number of controls per batch.
const DefaultBatchSize = 15

// Init creates a new AssessmentState from a gemara ControlCatalog.
// The catalog is the framework document — its controls become the control
// inventory. Returns a state ready for batched processing.
func Init(runID, program, framework, productName, productVersion, catalogPath, productSource string, batchSize int) (*state.AssessmentState, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	catalog, err := artifact.LoadControlCatalog(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("loading control catalog: %w", err)
	}

	s := &state.AssessmentState{
		SchemaVersion:  state.AssessmentStateSchemaVersion,
		RunID:          runID,
		Program:        program,
		Framework:      framework,
		ProductName:    productName,
		ProductVersion: productVersion,
		CatalogPath:    catalogPath,
		ProductSource:  productSource,
		Started:        time.Now().UTC(),
		LastUpdated:    time.Now().UTC(),
		Status:         state.StatusInProgress,
		BatchSize:      batchSize,
		Controls:       make(map[string]*state.ControlEntry),
	}

	// Extract control inventory from the catalog.
	controls := extractControls(catalog)
	if len(controls) == 0 {
		return nil, fmt.Errorf("no controls found in catalog at %q", catalogPath)
	}

	for _, c := range controls {
		s.Controls[c.ID] = c
	}

	// Build batches.
	s.Batches = buildBatches(controls, batchSize)
	s.RecalcSummary()

	return s, nil
}

// extractControls flattens a ControlCatalog into a slice of ControlEntries.
// Groups in go-gemara are referenced by ID in controls — they don't nest controls
// directly. All controls are in catalog.Controls; we group them by their Group
// field for organizational display purposes only.
func extractControls(catalog *gemara.ControlCatalog) []*state.ControlEntry {
	var entries []*state.ControlEntry

	for _, c := range catalog.Controls {
		entries = append(entries, controlToEntry(&c))
		// Assessment requirements become sub-entries.
		for _, req := range c.AssessmentRequirements {
			reqEntry := &state.ControlEntry{
				ID:          fmt.Sprintf("%s.%s", c.Id, req.Id),
				Title:       fmt.Sprintf("%s — Assessment Requirement %s", c.Title, req.Id),
				Requirement: req.Text,
				Status:      state.ControlPending,
			}
			entries = append(entries, reqEntry)
		}
	}

	return entries
}

func controlToEntry(c *gemara.Control) *state.ControlEntry {
	req := c.Objective
	if req == "" {
		req = c.Title
	}
	return &state.ControlEntry{
		ID:          c.Id,
		Title:       c.Title,
		Requirement: req,
		Status:      state.ControlPending,
	}
}

func controlRequirementText(c *gemara.Control) string { //nolint:unused // reserved for future batch prompts
	if c.Objective != "" {
		return c.Objective
	}
	return c.Title
}

func requirementText(req gemara.AssessmentRequirement) string { //nolint:unused // reserved for future batch prompts
	if req.Text != "" {
		return req.Text
	}
	return req.Id
}

// buildBatches groups controls into batches of size batchSize.
func buildBatches(controls []*state.ControlEntry, batchSize int) []*state.Batch {
	var batches []*state.Batch
	for i := 0; i < len(controls); i += batchSize {
		end := i + batchSize
		if end > len(controls) {
			end = len(controls)
		}
		batch := &state.Batch{
			Index:  len(batches),
			Status: state.StatusPending,
		}
		for _, c := range controls[i:end] {
			c.BatchIndex = batch.Index
			batch.ControlIDs = append(batch.ControlIDs, c.ID)
		}
		batches = append(batches, batch)
	}
	return batches
}

// ValidationResult is the 7-criterion validation result for a single control.
type ValidationResult struct {
	ControlID string
	Pass      bool
	Failures  []CriterionFailure
}

// CriterionFailure describes a single failed validation criterion.
type CriterionFailure struct {
	Criterion   int // 1-7
	Description string
}

// ValidateBatch validates all responses in a batch against the 7 criteria
// from the control assessment spec. Returns per-control results.
func ValidateBatch(s *state.AssessmentState, batch *state.Batch) []ValidationResult {
	var results []ValidationResult
	for _, id := range batch.ControlIDs {
		c, ok := s.Controls[id]
		if !ok {
			continue
		}
		if c.Response == nil {
			results = append(results, ValidationResult{
				ControlID: id,
				Pass:      false,
				Failures:  []CriterionFailure{{1, "no response provided"}},
			})
			continue
		}
		results = append(results, validateResponse(c))
	}
	return results
}

// validateResponse applies all 7 criteria to a single control's response.
func validateResponse(c *state.ControlEntry) ValidationResult {
	r := c.Response
	result := ValidationResult{ControlID: c.ID, Pass: true}

	// Criterion 1 — Citation present and specific.
	if r.Citation == "" {
		if r.Determination != state.NotSatisfied {
			result.Failures = append(result.Failures, CriterionFailure{1,
				"citation is missing but determination is not NotSatisfied"})
		}
	} else if r.Citation == "[CITATION NOT FOUND]" && r.Determination != state.NotSatisfied {
		result.Failures = append(result.Failures, CriterionFailure{1,
			"CITATION NOT FOUND must be paired with NotSatisfied determination"})
	}

	// Criterion 2 — Citation assertion test applied.
	if r.CitationQuality == "" {
		result.Failures = append(result.Failures, CriterionFailure{2,
			"citation_quality is missing; must be one of: direct_assertion, topical_reference, adjacent_capability, architectural_description, citation_not_found"})
	}
	if r.CitationQuality != state.DirectAssertion && r.CitationQuality != state.CitationNotFound &&
		r.Determination == state.Satisfied {
		result.Failures = append(result.Failures, CriterionFailure{2,
			fmt.Sprintf("determination is Satisfied but citation_quality is %q — only direct_assertion permits Satisfied", r.CitationQuality)})
	}

	// Criterion 3 — Inference boundary respected.
	for _, flag := range r.InferenceFlags {
		if r.Determination == state.Satisfied {
			result.Failures = append(result.Failures, CriterionFailure{3,
				fmt.Sprintf("inference flag %q present but determination is Satisfied — must be Partially Satisfied or lower", flag)})
			break
		}
	}
	// Check for unlabeled inference keywords in narrative.
	inferenceKeywords := []string{"likely", "should be", "can be", "may be", "presumably", "implies", "suggests"}
	for _, kw := range inferenceKeywords {
		if strings.Contains(strings.ToLower(r.Narrative), kw) && r.Determination == state.Satisfied {
			result.Failures = append(result.Failures, CriterionFailure{3,
				fmt.Sprintf("narrative contains hedging language %q with Satisfied determination — verify inference boundary", kw)})
			break
		}
	}

	// Criterion 4 — Coverage completeness test applied.
	// We can only verify this structurally: if narrative is empty, fail.
	if r.Narrative == "" {
		result.Failures = append(result.Failures, CriterionFailure{4, "narrative is empty"})
	}

	// Criterion 5 — Narrative directly addresses the requirement.
	// Structural check: narrative must be more than 10 words.
	if wordCount(r.Narrative) < 10 {
		result.Failures = append(result.Failures, CriterionFailure{5,
			"narrative is too short (< 10 words); must directly address the control requirement"})
	}

	// Criterion 6 — Determination is explicit and consistent with downgrade triggers.
	if r.Determination == "" {
		result.Failures = append(result.Failures, CriterionFailure{6, "determination is missing"})
	}
	if r.CitationQuality == state.ArchitecturalDescription && r.Determination != state.NotSatisfied {
		result.Failures = append(result.Failures, CriterionFailure{6,
			"citation_quality is architectural_description — determination must be NotSatisfied"})
	}

	// Criterion 7 — Gap noted where applicable.
	if (r.Determination == state.PartiallySatisfied || r.Determination == state.NotSatisfied) &&
		r.Gap == "" {
		result.Failures = append(result.Failures, CriterionFailure{7,
			fmt.Sprintf("determination is %q but gap description is missing", r.Determination)})
	}

	if len(result.Failures) > 0 {
		result.Pass = false
	}
	return result
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

// ApplyValidationResults updates the AssessmentState based on validation results.
// Controls that pass are marked Validated. Controls that fail have their
// ValidationFails count incremented. Controls that fail twice are marked Flagged.
func ApplyValidationResults(s *state.AssessmentState, results []ValidationResult) {
	for _, r := range results {
		c, ok := s.Controls[r.ControlID]
		if !ok {
			continue
		}
		if r.Pass {
			c.Status = state.ControlValidated
		} else {
			c.ValidationFails++
			if c.ValidationFails >= 2 {
				c.Status = state.ControlFlagged
			}
		}
	}
}

// AllBatchesComplete returns true if every batch in the state has status Complete.
func AllBatchesComplete(s *state.AssessmentState) bool {
	for _, b := range s.Batches {
		if b.Status != state.StatusComplete {
			return false
		}
	}
	return true
}
