// assay is a control assessment engine for the Formulary compliance
// micro-tools ecosystem.
//
// assay is the deterministic orchestration shell around a control assessment.
// It manages state, validates responses against the 7-criterion test, and
// assembles final output artifacts. The narrative writing is done by an AI
// agent or a human assessor — assay handles everything else.
//
// Subcommands:
//
//	assay init     Initialize a new assessment run from a gemara ControlCatalog
//	assay batch    Print the next batch of pending controls for an agent to fill
//	assay validate Apply 7-criterion validation to batch responses
//	assay assemble Assemble final output from a complete state file
//	assay status   Show current run status
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Formulary-Labs/assay/assessment"
	"github.com/Formulary-Labs/assay/output"
	"github.com/Formulary-Labs/assay/state"
	"github.com/Formulary-Labs/substrate/exit"
	"github.com/Formulary-Labs/substrate/provenance"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(exit.ToolError)
	}

	switch os.Args[1] {
	case "init":
		runInit(os.Args[2:])
	case "batch":
		runBatch(os.Args[2:])
	case "validate":
		runValidate(os.Args[2:])
	case "assemble":
		runAssemble(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "--version", "-v", "version":
		fmt.Printf("assay version %s\n", version)
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %q\n", os.Args[1])
		printUsage()
		os.Exit(exit.ToolError)
	}
}

// ── init ─────────────────────────────────────────────────────────────────────

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	var (
		framework   = fs.String("framework", "", "Framework type (iec62443-4-2, disa-stig, cis-benchmark, custom)")
		catalog     = fs.String("catalog", "", "Path to gemara ControlCatalog YAML")
		product     = fs.String("product", "", "Product documentation source (path or directory)")
		productName = fs.String("product-name", "", "Product display name")
		productVer  = fs.String("product-version", "", "Product version string")
		program     = fs.String("program", "", "Program slug")
		batchSize   = fs.Int("batch-size", assessment.DefaultBatchSize, "Controls per batch (default 15)")
		outputDir   = fs.String("output-dir", "", "Output directory (default: data/[program]/assessments/[run-id])")
		runID       = fs.String("run-id", "", "Run ID (auto-generated if empty)")
	)
	fs.Parse(args) //nolint:errcheck

	if *catalog == "" {
		fmt.Fprintln(os.Stderr, "error: --catalog is required")
		os.Exit(exit.ToolError)
	}
	if *productName == "" {
		fmt.Fprintln(os.Stderr, "error: --product-name is required")
		os.Exit(exit.ToolError)
	}
	if *program == "" {
		fmt.Fprintln(os.Stderr, "error: --program is required")
		os.Exit(exit.ToolError)
	}

	if *runID == "" {
		*runID = fmt.Sprintf("%s-%s-%s", time.Now().UTC().Format("2006-01-02"), sanitize(*framework), sanitize(*productName))
	}

	if *outputDir == "" {
		*outputDir = filepath.Join("data", *program, "assessments", *runID)
	}

	s, err := assessment.Init(*runID, *program, *framework, *productName, *productVer, *catalog, *product, *batchSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"error": %q, "code": 2}`+"\n", err.Error())
		os.Exit(exit.ToolError)
	}

	statePath := filepath.Join("data", *program, "assessments", *runID+"-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating state directory: %v\n", err)
		os.Exit(exit.ToolError)
	}
	if err := s.Save(statePath); err != nil {
		fmt.Fprintf(os.Stderr, "error saving state: %v\n", err)
		os.Exit(exit.ToolError)
	}

	result := map[string]interface{}{
		"run_id":     s.RunID,
		"state_path": statePath,
		"controls":   len(s.Controls),
		"batches":    len(s.Batches),
		"batch_size": s.BatchSize,
		"status":     s.Status,
	}
	printJSON(result)

	_ = provenance.Write("logs/provenance.jsonl", provenance.Entry{
		Spec:        "functions/control-assessment-spec.md",
		Output:      statePath,
		OutputType:  "other",
		Program:     *program,
		Purpose:     fmt.Sprintf("assay init: %s %s — %d controls in %d batches", *productName, *productVer, len(s.Controls), len(s.Batches)),
		Reusability: provenance.Instance,
		QualityGate: provenance.Pass,
		Tool:        "assay",
		ToolVersion: version,
	})
}

// ── batch ─────────────────────────────────────────────────────────────────────

func runBatch(args []string) {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	var (
		statePath  = fs.String("state", "", "Path to state file")
		batchIndex = fs.Int("batch", -1, "Batch index (default: next pending batch)")
	)
	fs.Parse(args) //nolint:errcheck

	if *statePath == "" {
		fmt.Fprintln(os.Stderr, "error: --state is required")
		os.Exit(exit.ToolError)
	}

	s, err := state.Load(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading state: %v\n", err)
		os.Exit(exit.ToolError)
	}

	var batch *state.Batch
	if *batchIndex >= 0 {
		if *batchIndex < len(s.Batches) {
			batch = s.Batches[*batchIndex]
		}
	} else {
		batch = s.NextBatch()
	}

	if batch == nil {
		fmt.Println(`{"message": "all batches complete", "pending": 0}`)
		os.Exit(exit.OK)
	}

	controls := s.PendingControls(batch)

	type batchOutput struct {
		RunID      string                 `json:"run_id"`
		BatchIndex int                    `json:"batch_index"`
		Total      int                    `json:"total_batches"`
		Controls   []*state.ControlEntry  `json:"controls"`
		Instructions string               `json:"instructions"`
	}

	out := batchOutput{
		RunID:      s.RunID,
		BatchIndex: batch.Index,
		Total:      len(s.Batches),
		Controls:   controls,
		Instructions: "For each control: fill response.citation, response.citation_quality, response.narrative, response.determination, response.gap (if applicable). Then call: assay validate --state [path] --responses [responses.json]",
	}
	printJSON(out)
}

// ── validate ──────────────────────────────────────────────────────────────────

func runValidate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	var (
		statePath     = fs.String("state", "", "Path to state file")
		responsesPath = fs.String("responses", "", "Path to JSON file with filled responses")
	)
	fs.Parse(args) //nolint:errcheck

	if *statePath == "" || *responsesPath == "" {
		fmt.Fprintln(os.Stderr, "error: --state and --responses are required")
		os.Exit(exit.ToolError)
	}

	s, err := state.Load(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading state: %v\n", err)
		os.Exit(exit.ToolError)
	}

	// Load responses.
	respData, err := os.ReadFile(*responsesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading responses: %v\n", err)
		os.Exit(exit.ToolError)
	}
	var responses map[string]*state.Response
	if err := json.Unmarshal(respData, &responses); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing responses: %v\n", err)
		os.Exit(exit.ToolError)
	}

	// Find the batch these responses belong to. Use the first control ID.
	var batchIdx int = -1
	for id := range responses {
		if c, ok := s.Controls[id]; ok {
			batchIdx = c.BatchIndex
			break
		}
	}
	if batchIdx < 0 || batchIdx >= len(s.Batches) {
		fmt.Fprintln(os.Stderr, "error: could not determine batch from responses")
		os.Exit(exit.ToolError)
	}
	batch := s.Batches[batchIdx]

	// Apply responses to state.
	for id, resp := range responses {
		if c, ok := s.Controls[id]; ok {
			c.Response = resp
		}
	}

	// Validate.
	results := assessment.ValidateBatch(s, batch)
	assessment.ApplyValidationResults(s, results)

	// Check if batch is fully validated.
	allPass := true
	for _, r := range results {
		if !r.Pass {
			allPass = false
			break
		}
	}
	if allPass {
		batch.Status = state.StatusComplete
	}

	s.RecalcSummary()
	if err := s.Save(*statePath); err != nil {
		fmt.Fprintf(os.Stderr, "error saving state: %v\n", err)
		os.Exit(exit.ToolError)
	}

	printJSON(map[string]interface{}{
		"batch_index":  batchIdx,
		"total":        len(results),
		"passed":       countPass(results),
		"failed":       len(results) - countPass(results),
		"batch_status": batch.Status,
		"results":      results,
	})

	if !allPass {
		os.Exit(exit.Validation)
	}
}

// ── assemble ──────────────────────────────────────────────────────────────────

func runAssemble(args []string) {
	fs := flag.NewFlagSet("assemble", flag.ExitOnError)
	var (
		statePath = fs.String("state", "", "Path to state file")
		outputDir = fs.String("output-dir", "", "Output directory (default: same dir as state file)")
	)
	fs.Parse(args) //nolint:errcheck

	if *statePath == "" {
		fmt.Fprintln(os.Stderr, "error: --state is required")
		os.Exit(exit.ToolError)
	}

	s, err := state.Load(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading state: %v\n", err)
		os.Exit(exit.ToolError)
	}

	dir := *outputDir
	if dir == "" {
		dir = filepath.Dir(*statePath)
	}

	result, err := output.Assemble(s, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error assembling output: %v\n", err)
		os.Exit(exit.ToolError)
	}

	s.Status = state.StatusComplete
	s.RecalcSummary()
	s.Save(*statePath) //nolint:errcheck

	printJSON(map[string]interface{}{
		"run_id":            s.RunID,
		"markdown_artifact": result.MarkdownArtifact,
		"gap_report":        result.GapReport,
		"json_artifact":     result.JSONArtifact,
		"summary":           s.Summary,
	})

	_ = provenance.Write("logs/provenance.jsonl", provenance.Entry{
		Spec:        "functions/control-assessment-spec.md",
		Output:      result.MarkdownArtifact,
		OutputType:  "other",
		Program:     s.Program,
		Purpose:     fmt.Sprintf("assay assemble: %s %s — %d controls", s.ProductName, s.ProductVersion, s.Summary.Total),
		Reusability: provenance.Artifact,
		QualityGate: provenance.Pass,
		Tool:        "assay",
		ToolVersion: version,
	})
}

// ── status ────────────────────────────────────────────────────────────────────

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	var statePath = fs.String("state", "", "Path to state file")
	fs.Parse(args) //nolint:errcheck

	if *statePath == "" {
		fmt.Fprintln(os.Stderr, "error: --state is required")
		os.Exit(exit.ToolError)
	}

	s, err := state.Load(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading state: %v\n", err)
		os.Exit(exit.ToolError)
	}
	s.RecalcSummary()

	next := s.NextBatch()
	nextBatch := -1
	if next != nil {
		nextBatch = next.Index
	}

	printJSON(map[string]interface{}{
		"run_id":       s.RunID,
		"status":       s.Status,
		"started":      s.Started,
		"last_updated": s.LastUpdated,
		"summary":      s.Summary,
		"next_batch":   nextBatch,
		"total_batches": len(s.Batches),
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v) //nolint:errcheck
}

func countPass(results []assessment.ValidationResult) int {
	n := 0
	for _, r := range results {
		if r.Pass {
			n++
		}
	}
	return n
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, r)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `assay — control assessment engine

Usage:
  assay <subcommand> [flags]

Subcommands:
  init       Initialize a new assessment run from a gemara ControlCatalog
  batch      Print the next batch of pending controls (for agent/human to fill)
  validate   Apply 7-criterion validation to a set of filled responses
  assemble   Assemble final output artifacts from a complete state file
  status     Show current run status and progress
  version    Print version and exit

assay is AI-optional: a human assessor or AI agent fills in the response
narratives; assay handles orchestration, validation, and output assembly.

Examples:
  # Initialize a new assessment
  assay init --catalog catalog.yaml --product-name "MyProduct" \
    --product-version "1.0" --framework iec62443-4-2 --program myprogram

  # Get next batch for an agent to fill
  assay batch --state data/myprogram/assessments/2026-01-01-iec62443-4-2-myproduct-state.json

  # Validate filled responses
  assay validate --state state.json --responses responses.json

  # Assemble final outputs
  assay assemble --state state.json`)
}
