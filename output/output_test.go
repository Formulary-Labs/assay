package output_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Formulary-Labs/assay/output"
	"github.com/Formulary-Labs/assay/state"
)

func makeTestState() *state.AssessmentState {
	s := &state.AssessmentState{
		RunID:          "test-run-001",
		Program:        "test-program",
		Framework:      "iso42001",
		ProductName:    "test-product",
		ProductVersion: "1.0",
		CatalogPath:    "catalog.yaml",
		ProductSource:  "product-docs/",
		Started:        time.Now(),
		Status:         state.StatusComplete,
		Controls: map[string]*state.ControlEntry{
			"A.6.1": {
				ID:          "A.6.1",
				Title:       "AI Policy",
				Requirement: "Establish and maintain a policy for AI use.",
				Status:      state.ControlPending,
			},
			"A.6.2": {
				ID:          "A.6.2",
				Title:       "AI Risk Register",
				Requirement: "Maintain a risk register for AI systems.",
				Status:      state.ControlValidated,
				Response: &state.Response{
					Determination:   state.Satisfied,
					CitationQuality: state.DirectAssertion,
					Citation:        "Section 4.3.1",
					Narrative:       "Risk register is maintained and reviewed quarterly.",
				},
			},
			"A.6.3": {
				ID:          "A.6.3",
				Title:       "AI Transparency",
				Requirement: "Document AI system decisions.",
				Status:      state.ControlValidated,
				Response: &state.Response{
					Determination:   state.NotSatisfied,
					CitationQuality: state.CitationNotFound,
					Gap:             "No documentation found.",
					RemediationPath: "Create transparency report.",
				},
			},
		},
		Batches: []*state.Batch{
			{
				Index:      0,
				Status:     state.StatusComplete,
				ControlIDs: []string{"A.6.1", "A.6.2", "A.6.3"},
			},
		},
	}
	s.RecalcSummary()
	return s
}

func TestAssemble_createsAllFiles(t *testing.T) {
	s := makeTestState()
	dir := t.TempDir()
	result, err := output.Assemble(s, dir)
	if err != nil {
		t.Fatalf("Assemble error: %v", err)
	}

	for _, path := range []string{result.MarkdownArtifact, result.GapReport, result.JSONArtifact} {
		if path == "" {
			t.Error("output path must not be empty")
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("expected file %q to exist: %v", path, statErr)
		}
	}
}

func TestAssemble_markdownContainsControls(t *testing.T) {
	s := makeTestState()
	dir := t.TempDir()
	result, err := output.Assemble(s, dir)
	if err != nil {
		t.Fatalf("Assemble error: %v", err)
	}

	data, err := os.ReadFile(result.MarkdownArtifact)
	if err != nil {
		t.Fatalf("reading markdown artifact: %v", err)
	}
	md := string(data)

	for _, want := range []string{"test-product", "iso42001", "A.6.2", "A.6.3"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestAssemble_gapReportContainsNotSatisfied(t *testing.T) {
	s := makeTestState()
	dir := t.TempDir()
	result, err := output.Assemble(s, dir)
	if err != nil {
		t.Fatalf("Assemble error: %v", err)
	}

	data, err := os.ReadFile(result.GapReport)
	if err != nil {
		t.Fatalf("reading gap report: %v", err)
	}
	gap := string(data)

	if !strings.Contains(gap, "A.6.3") {
		t.Error("gap report missing not-satisfied control A.6.3")
	}
	if !strings.Contains(gap, "Citation Not Found") {
		t.Error("gap report missing citation-not-found section")
	}
}

func TestAssemble_jsonRoundTrip(t *testing.T) {
	s := makeTestState()
	dir := t.TempDir()
	result, err := output.Assemble(s, dir)
	if err != nil {
		t.Fatalf("Assemble error: %v", err)
	}

	data, err := os.ReadFile(result.JSONArtifact)
	if err != nil {
		t.Fatalf("reading json artifact: %v", err)
	}

	var loaded state.AssessmentState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if loaded.ProductName != s.ProductName {
		t.Errorf("round-trip ProductName = %q, want %q", loaded.ProductName, s.ProductName)
	}
	if loaded.Framework != s.Framework {
		t.Errorf("round-trip Framework = %q, want %q", loaded.Framework, s.Framework)
	}
}

func TestAssemble_filenamesContainProductAndFramework(t *testing.T) {
	s := makeTestState()
	dir := t.TempDir()
	result, err := output.Assemble(s, dir)
	if err != nil {
		t.Fatalf("Assemble error: %v", err)
	}

	base := filepath.Base(result.MarkdownArtifact)
	if !strings.Contains(base, "test-product") {
		t.Errorf("markdown filename %q should contain product name", base)
	}
	if !strings.Contains(base, "iso42001") {
		t.Errorf("markdown filename %q should contain framework", base)
	}
}

func TestAssemble_dryRunOutputDir(t *testing.T) {
	s := makeTestState()
	dir := t.TempDir()
	// Assemble to a nested subdir that doesn't exist yet.
	nested := filepath.Join(dir, "subdir", "nested")
	result, err := output.Assemble(s, nested)
	if err != nil {
		t.Fatalf("Assemble should create output dir: %v", err)
	}
	if _, statErr := os.Stat(result.MarkdownArtifact); statErr != nil {
		t.Errorf("markdown artifact not created in nested dir: %v", statErr)
	}
}
