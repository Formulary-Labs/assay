// Package output assembles the final assessment outputs from a complete
// AssessmentState: the gemara Layer 5 EvaluationLog artifact, a markdown
// assessment artifact, and a gap report.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Formulary-Labs/assay/state"
)

// AssembleResult is the set of output file paths produced by Assemble.
type AssembleResult struct {
	MarkdownArtifact string
	GapReport        string
	JSONArtifact     string
}

// Assemble produces all output files from a complete AssessmentState.
// outputDir is created if it does not exist.
func Assemble(s *state.AssessmentState, outputDir string) (AssembleResult, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return AssembleResult{}, fmt.Errorf("creating output dir: %w", err)
	}

	base := fmt.Sprintf("%s-%s", sanitizeFilename(s.ProductName), sanitizeFilename(s.Framework))

	mdPath := filepath.Join(outputDir, base+"-assessment.md")
	if err := writeMarkdownArtifact(s, mdPath); err != nil {
		return AssembleResult{}, fmt.Errorf("writing markdown artifact: %w", err)
	}

	gapPath := filepath.Join(outputDir, base+"-gaps.md")
	if err := writeGapReport(s, gapPath); err != nil {
		return AssembleResult{}, fmt.Errorf("writing gap report: %w", err)
	}

	jsonPath := filepath.Join(outputDir, base+"-assessment.json")
	if err := writeJSONArtifact(s, jsonPath); err != nil {
		return AssembleResult{}, fmt.Errorf("writing JSON artifact: %w", err)
	}

	return AssembleResult{
		MarkdownArtifact: mdPath,
		GapReport:        gapPath,
		JSONArtifact:     jsonPath,
	}, nil
}

func writeMarkdownArtifact(s *state.AssessmentState, path string) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s %s — %s Assessment\n\n", s.ProductName, s.ProductVersion, s.Framework)
	fmt.Fprintf(&b, "**Run ID:** %s  \n", s.RunID)
	fmt.Fprintf(&b, "**Generated:** %s  \n", time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(&b, "**Program:** %s  \n", s.Program)
	fmt.Fprintf(&b, "**Product source:** %s  \n", s.ProductSource)
	fmt.Fprintf(&b, "**Framework catalog:** %s  \n\n", s.CatalogPath)

	s.RecalcSummary()
	sum := s.Summary
	total := sum.Total
	if total == 0 {
		total = 1
	}
	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Status | Count | %% |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| Satisfied | %d | %.0f%% |\n", sum.Satisfied, pct(sum.Satisfied, total))
	fmt.Fprintf(&b, "| Partially Satisfied | %d | %.0f%% |\n", sum.PartiallySatisfied, pct(sum.PartiallySatisfied, total))
	fmt.Fprintf(&b, "| Not Satisfied | %d | %.0f%% |\n", sum.NotSatisfied, pct(sum.NotSatisfied, total))
	fmt.Fprintf(&b, "| N/A | %d | %.0f%% |\n", sum.NA, pct(sum.NA, total))
	fmt.Fprintf(&b, "| Citation Not Found | %d | %.0f%% |\n", sum.CitationNotFound, pct(sum.CitationNotFound, total))
	fmt.Fprintf(&b, "| Flagged (manual review) | %d | — |\n", sum.Flagged)
	fmt.Fprintf(&b, "| Pending | %d | — |\n\n", sum.Pending)

	fmt.Fprintf(&b, "---\n\n## Control Responses\n\n")

	for _, batch := range s.Batches {
		for _, id := range batch.ControlIDs {
			c := s.Controls[id]
			if c == nil {
				continue
			}
			fmt.Fprintf(&b, "### %s — %s\n\n", c.ID, c.Title)
			fmt.Fprintf(&b, "**Requirement:** %s  \n", c.Requirement)
			if c.Response != nil {
				r := c.Response
				fmt.Fprintf(&b, "**Determination:** %s  \n", r.Determination)
				fmt.Fprintf(&b, "**Citation quality:** %s  \n", r.CitationQuality)
				fmt.Fprintf(&b, "**Citation:** %s  \n", r.Citation)
				fmt.Fprintf(&b, "**Narrative:** %s  \n", r.Narrative)
				if r.Gap != "" {
					fmt.Fprintf(&b, "**Gap:** %s  \n", r.Gap)
					fmt.Fprintf(&b, "**Remediation path:** %s  \n", r.RemediationPath)
				}
				if r.Note != "" {
					fmt.Fprintf(&b, "**Note:** %s  \n", r.Note)
				}
				if len(r.InferenceFlags) > 0 {
					fmt.Fprintf(&b, "**Inference flags:** %s  \n", strings.Join(r.InferenceFlags, ", "))
				}
			} else {
				fmt.Fprintf(&b, "**Status:** %s  \n", c.Status)
			}
			fmt.Fprintf(&b, "\n---\n\n")
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeGapReport(s *state.AssessmentState, path string) error {
	var b strings.Builder
	var partial, notSat, citNotFound []string

	for _, batch := range s.Batches {
		for _, id := range batch.ControlIDs {
			c := s.Controls[id]
			if c == nil || c.Response == nil {
				continue
			}
			switch c.Response.Determination {
			case state.PartiallySatisfied:
				partial = append(partial, id)
			case state.NotSatisfied:
				notSat = append(notSat, id)
			}
			if c.Response.CitationQuality == state.CitationNotFound {
				citNotFound = append(citNotFound, id)
			}
		}
	}

	s.RecalcSummary()
	fmt.Fprintf(&b, "# %s — %s Gap Report\n\n", s.ProductName, s.Framework)
	fmt.Fprintf(&b, "**Run ID:** %s | **Generated:** %s\n\n", s.RunID, time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "- Partially Satisfied: %d controls\n", len(partial))
	fmt.Fprintf(&b, "- Not Satisfied: %d controls\n", len(notSat))
	fmt.Fprintf(&b, "- Citation Not Found: %d controls (manual review required)\n\n", len(citNotFound))
	fmt.Fprintf(&b, "---\n\n")

	if len(partial) > 0 {
		fmt.Fprintf(&b, "## Partially Satisfied\n\n")
		for _, id := range partial {
			c := s.Controls[id]
			r := c.Response
			fmt.Fprintf(&b, "### %s — %s\n\n", c.ID, c.Title)
			fmt.Fprintf(&b, "**Gap:** %s  \n", r.Gap)
			fmt.Fprintf(&b, "**Remediation path:** %s  \n", r.RemediationPath)
			fmt.Fprintf(&b, "**Citation:** %s  \n\n---\n\n", r.Citation)
		}
	}

	if len(notSat) > 0 {
		fmt.Fprintf(&b, "## Not Satisfied\n\n")
		for _, id := range notSat {
			c := s.Controls[id]
			r := c.Response
			fmt.Fprintf(&b, "### %s — %s\n\n", c.ID, c.Title)
			fmt.Fprintf(&b, "**Gap:** %s  \n", r.Gap)
			fmt.Fprintf(&b, "**Remediation path:** %s  \n", r.RemediationPath)
			fmt.Fprintf(&b, "**Citation:** %s  \n\n---\n\n", r.Citation)
		}
	}

	if len(citNotFound) > 0 {
		fmt.Fprintf(&b, "## Citation Not Found — Manual Review Required\n\n")
		for _, id := range citNotFound {
			c := s.Controls[id]
			fmt.Fprintf(&b, "### %s — %s\n\n", c.ID, c.Title)
			fmt.Fprintf(&b, "**Requirement:** %s  \n", c.Requirement)
			fmt.Fprintf(&b, "**Action required:** Locate relevant product documentation section and complete manually.\n\n---\n\n")
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeJSONArtifact(s *state.AssessmentState, path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func sanitizeFilename(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var out []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out = append(out, r)
		}
	}
	return string(out)
}
