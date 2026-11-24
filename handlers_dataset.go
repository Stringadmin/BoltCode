package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/dataset"
	"github.com/Stringadmin/BoltCode/internal/fio"
)

func runDataset(wd string, args []string) error {
	if len(args) == 0 || args[0] != "import-opencode" {
		return fmt.Errorf("usage: boltcode dataset import-opencode --project <dir> --output <file> [--session <id>] [--opencode <path>]")
	}

	flags := flag.NewFlagSet("dataset import-opencode", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	project := flags.String("project", wd, "OpenCode project directory")
	output := flags.String("output", "", "destination JSONL file")
	sessions := flags.String("session", "", "optional comma-separated OpenCode session ids")
	opencode := flags.String("opencode", "", "optional OpenCode executable path")
	maxCount := flags.Int("max-count", 0, "limit recent sessions inspected")
	force := flags.Bool("force", false, "replace an existing output file")
	rawSource := flags.Bool("raw-source", false, "read local source before BoltCode redaction (authorized projects only)")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("parsing dataset flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected dataset arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*output) == "" {
		return fmt.Errorf("--output is required")
	}
	projectDir, err := filepath.Abs(*project)
	if err != nil {
		return fmt.Errorf("resolving project directory: %w", err)
	}
	if info, statErr := os.Stat(projectDir); statErr != nil || !info.IsDir() {
		if statErr != nil {
			return fmt.Errorf("opening project directory: %w", statErr)
		}
		return fmt.Errorf("project path is not a directory: %s", projectDir)
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}
	if _, statErr := os.Stat(outputPath); statErr == nil && !*force {
		return fmt.Errorf("output already exists: %s (use --force to replace it)", outputPath)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("checking output path: %w", statErr)
	}

	var sessionIDs []string
	if strings.TrimSpace(*sessions) != "" {
		sessionIDs = strings.Split(*sessions, ",")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	traces, summary, err := dataset.ImportOpenCode(ctx, dataset.OpenCodeOptions{
		Executable:     *opencode,
		ProjectDir:     projectDir,
		SessionIDs:     sessionIDs,
		MaxCount:       *maxCount,
		AllowRawSource: *rawSource,
	})
	if err != nil {
		return err
	}
	data, err := dataset.MarshalJSONL(traces)
	if err != nil {
		return fmt.Errorf("encoding dataset: %w", err)
	}
	if err := fio.WriteFileAtomic(outputPath, data, 0o600); err != nil {
		return fmt.Errorf("writing dataset: %w", err)
	}

	fmt.Printf("dataset written: %s\n", outputPath)
	if *rawSource {
		fmt.Println("  source mode: raw local export + BoltCode redaction (reasoning excluded)")
	} else {
		fmt.Println("  source mode: OpenCode sanitized export")
	}
	fmt.Printf("  sessions: %d  messages: %d  text: %d  tools: %d  tool-errors: %d\n",
		summary.Sessions, summary.Messages, summary.TextParts, summary.ToolExecutions, summary.ToolErrors)
	fmt.Printf("  dropped reasoning: %d  redactions: %d\n", summary.DroppedReasoning, summary.Redactions)
	return nil
}
