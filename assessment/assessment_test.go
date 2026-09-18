package assessment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Formulary-Labs/assay/assessment"
	"github.com/Formulary-Labs/assay/state"
)

const testCatalogPath = "../testdata/test-catalog.yaml"

func TestInit(t *testing.T) {
	if _, err := os.Stat(testCatalogPath); err != nil {
		t.Skip("test catalog not found — skipping")
	}

	s, err := assessment.Init(
		"test-run-001", "test-program", "iec62443-4-2",
		"TestProduct", "1.0", testCatalogPath, "docs/",
		assessment.DefaultBatchSize,
	)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if len(s.Controls) == 0 {
		t.Error("no controls extracted from catalog")
	}
	if len(s.Batches) == 0 {
		t.Error("no batches created")
	}
	if s.RunID != "test-run-001" {
		t.Errorf("run_id = %q, want %q", s.RunID, "test-run-001")
	}
}

func TestValidateBatch_pass(t *testing.T) {
	s := &state.AssessmentState{
		RunID: "test",
		Controls: map[string]*state.ControlEntry{
			"SR.1.1": {
				ID:         "SR.1.1",
				Title:      "User Identification",
				Requirement: "The system shall uniquely identify all users.",
				BatchIndex: 0,
				Status:     state.ControlPending,
				Response: &state.Response{
					Citation:        "Product Docs v1.0, Section 4.2.1: \"User Authentication\"",
					CitationQuality: state.DirectAssertion,
					Narrative:       "Section 4.2.1 explicitly states that every user is assigned a unique identifier at account creation. The system enforces uniqueness across all active and archived accounts, preventing identifier reuse.",
					Determination:   state.Satisfied,
				},
			},
		},
		Batches: []*state.Batch{
			{Index: 0, ControlIDs: []string{"SR.1.1"}, Status: state.StatusPending},
		},
	}

	results := assessment.ValidateBatch(s, s.Batches[0])
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Pass {
		t.Errorf("expected pass, got failures: %v", results[0].Failures)
	}
}

func TestValidateBatch_failCriterion2(t *testing.T) {
	s := &state.AssessmentState{
		Controls: map[string]*state.ControlEntry{
			"SR.1.1": {
				ID:         "SR.1.1",
				BatchIndex: 0,
				Status:     state.ControlPending,
				Response: &state.Response{
					Citation:        "Section 4.2.1",
					CitationQuality: state.TopicalReference, // not direct assertion
					Narrative:       "Section 4.2.1 discusses authentication mechanisms used by the product including unique identifiers.",
					Determination:   state.Satisfied, // but satisfied — this should fail criterion 2
				},
			},
		},
		Batches: []*state.Batch{
			{Index: 0, ControlIDs: []string{"SR.1.1"}, Status: state.StatusPending},
		},
	}

	results := assessment.ValidateBatch(s, s.Batches[0])
	if len(results) != 1 || results[0].Pass {
		t.Errorf("expected failure for TopicalReference + Satisfied, got pass")
	}
	foundC2 := false
	for _, f := range results[0].Failures {
		if f.Criterion == 2 {
			foundC2 = true
		}
	}
	if !foundC2 {
		t.Errorf("expected criterion 2 failure, got: %v", results[0].Failures)
	}
}

func TestValidateBatch_failCriterion7(t *testing.T) {
	s := &state.AssessmentState{
		Controls: map[string]*state.ControlEntry{
			"SR.1.1": {
				ID:         "SR.1.1",
				BatchIndex: 0,
				Status:     state.ControlPending,
				Response: &state.Response{
					Citation:        "Section 4.2.1: \"Authentication\"",
					CitationQuality: state.DirectAssertion,
					Narrative:       "The product provides user identification as described in section 4.2.1 of the documentation.",
					Determination:   state.PartiallySatisfied,
					// Gap missing — should fail criterion 7
				},
			},
		},
		Batches: []*state.Batch{
			{Index: 0, ControlIDs: []string{"SR.1.1"}, Status: state.StatusPending},
		},
	}

	results := assessment.ValidateBatch(s, s.Batches[0])
	foundC7 := false
	for _, f := range results[0].Failures {
		if f.Criterion == 7 {
			foundC7 = true
		}
	}
	if !foundC7 {
		t.Errorf("expected criterion 7 failure (missing gap), got: %v", results[0].Failures)
	}
}

func TestStateSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-state.json")

	s := &state.AssessmentState{
		RunID:   "test-001",
		Program: "test-program",
		Status:  state.StatusInProgress,
		Controls: map[string]*state.ControlEntry{
			"SR.1.1": {ID: "SR.1.1", Title: "Test Control", Status: state.ControlPending},
		},
		Batches: []*state.Batch{
			{Index: 0, ControlIDs: []string{"SR.1.1"}, Status: state.StatusPending},
		},
	}

	if err := s.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := state.Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.RunID != s.RunID {
		t.Errorf("RunID = %q, want %q", loaded.RunID, s.RunID)
	}
	if len(loaded.Controls) != 1 {
		t.Errorf("Controls count = %d, want 1", len(loaded.Controls))
	}
}
