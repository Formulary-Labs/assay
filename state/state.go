// Package state defines the AssessmentState — the resumable checkpoint
// written after every validated batch. The state file is the source of truth
// for all assay operations: init, batch, validate, and assemble all read and
// write through this type.
//
// File path convention: data/[program]/assessments/[RUN_ID]-state.json
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Status is the overall run status.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusComplete   Status = "complete"
	StatusFailed     Status = "failed"
)

// ControlStatus is the per-control processing status.
type ControlStatus string

const (
	ControlPending    ControlStatus = "pending"
	ControlProcessing ControlStatus = "processing"
	ControlValidated  ControlStatus = "validated"
	ControlFlagged    ControlStatus = "flagged" // failed validation twice — needs manual review
)

// Determination is the satisfaction determination for a control.
type Determination string

const (
	Satisfied         Determination = "satisfied"
	PartiallySatisfied Determination = "partially_satisfied"
	NotSatisfied      Determination = "not_satisfied"
	NA                Determination = "na"
)

// CitationQuality is the quality classification for a citation.
type CitationQuality string

const (
	DirectAssertion         CitationQuality = "direct_assertion"
	TopicalReference        CitationQuality = "topical_reference"
	AdjacentCapability      CitationQuality = "adjacent_capability"
	ArchitecturalDescription CitationQuality = "architectural_description"
	CitationNotFound        CitationQuality = "citation_not_found"
)

// AssessmentState is the full persistent state for a single assessment run.
type AssessmentState struct {
	RunID          string                     `json:"run_id"`
	Program        string                     `json:"program"`
	Framework      string                     `json:"framework"`
	ProductName    string                     `json:"product_name"`
	ProductVersion string                     `json:"product_version"`
	CatalogPath    string                     `json:"catalog_path"`
	ProductSource  string                     `json:"product_source"`
	Started        time.Time                  `json:"started"`
	LastUpdated    time.Time                  `json:"last_updated"`
	Status         Status                     `json:"status"`
	BatchSize      int                        `json:"batch_size"`
	Controls       map[string]*ControlEntry   `json:"controls"`
	Batches        []*Batch                   `json:"batches"`
	Summary        Summary                    `json:"summary"`
}

// ControlEntry is a single control in the inventory.
type ControlEntry struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Requirement    string        `json:"requirement"`
	Severity       string        `json:"severity,omitempty"`  // CAT I/II/III, SL level, etc.
	BatchIndex     int           `json:"batch_index"`
	Status         ControlStatus `json:"status"`
	Response       *Response     `json:"response,omitempty"`
	ValidationFails int          `json:"validation_fails,omitempty"`
}

// Response is a single control's assessment response.
type Response struct {
	Citation         string          `json:"citation"`
	CitationQuality  CitationQuality `json:"citation_quality"`
	Narrative        string          `json:"narrative"`
	Determination    Determination   `json:"determination"`
	Gap              string          `json:"gap,omitempty"`
	RemediationPath  string          `json:"remediation_path,omitempty"`
	Note             string          `json:"note,omitempty"`
	InferenceFlags   []string        `json:"inference_flags,omitempty"`
}

// Batch is a processing unit of controls.
type Batch struct {
	Index      int      `json:"index"`
	ControlIDs []string `json:"control_ids"`
	Status     Status   `json:"status"`
}

// Summary is aggregate counts across all controls.
type Summary struct {
	Total              int `json:"total"`
	Satisfied          int `json:"satisfied"`
	PartiallySatisfied int `json:"partially_satisfied"`
	NotSatisfied       int `json:"not_satisfied"`
	NA                 int `json:"na"`
	CitationNotFound   int `json:"citation_not_found"`
	Pending            int `json:"pending"`
	Flagged            int `json:"flagged"`
}

// Load reads a state file from disk.
func Load(path string) (*AssessmentState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading state file %q: %w", path, err)
	}
	var s AssessmentState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file %q: %w", path, err)
	}
	return &s, nil
}

// Save writes the state file to disk (atomic: write to temp then rename).
func (s *AssessmentState) Save(path string) error {
	s.LastUpdated = time.Now().UTC()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing state temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming state temp file: %w", err)
	}
	return nil
}

// NextBatch returns the first batch whose status is not complete.
// Returns nil if all batches are complete.
func (s *AssessmentState) NextBatch() *Batch {
	for _, b := range s.Batches {
		if b.Status != StatusComplete {
			return b
		}
	}
	return nil
}

// PendingControls returns all controls in the given batch that are pending.
func (s *AssessmentState) PendingControls(batch *Batch) []*ControlEntry {
	var out []*ControlEntry
	for _, id := range batch.ControlIDs {
		if c, ok := s.Controls[id]; ok && c.Status == ControlPending {
			out = append(out, c)
		}
	}
	return out
}

// RecalcSummary recalculates the Summary from the current control states.
func (s *AssessmentState) RecalcSummary() {
	s.Summary = Summary{Total: len(s.Controls)}
	for _, c := range s.Controls {
		switch c.Status {
		case ControlPending, ControlProcessing:
			s.Summary.Pending++
		case ControlFlagged:
			s.Summary.Flagged++
		case ControlValidated:
			if c.Response == nil {
				continue
			}
			switch c.Response.Determination {
			case Satisfied:
				s.Summary.Satisfied++
			case PartiallySatisfied:
				s.Summary.PartiallySatisfied++
			case NotSatisfied:
				s.Summary.NotSatisfied++
			case NA:
				s.Summary.NA++
			}
			if c.Response.CitationQuality == CitationNotFound {
				s.Summary.CitationNotFound++
			}
		}
	}
}
