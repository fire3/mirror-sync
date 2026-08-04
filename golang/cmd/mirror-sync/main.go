package main

import (
	"fmt"
	"os"

	"github.com/user/mirror-sync/internal/config"
	"github.com/user/mirror-sync/internal/runner"
	"github.com/user/mirror-sync/internal/tui"
	"github.com/user/mirror-sync/types"
)

func main() {
	// Parse CLI flags
	cfg, useTUI := parseFlags()

	if useTUI {
		if err := tui.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Run in CLI mode
	if err := runCLI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() (types.AppConfig, bool) {
	var (
		taskStr      string
		metadataDate string
		outputDate   string
		outputDir    string
		oldDate      string
		newDate      string
		concurrency  int
		retry        int
		timeout      int
		simpleURL    string
	)

	// Check for --help or -h before flag.Parse
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "-h" {
			printUsage()
			os.Exit(0)
		}
		if arg == "--tui" || arg == "-t" {
			return types.AppConfig{}, true
		}
	}

	// No args → TUI mode
	if len(os.Args) == 1 {
		return types.AppConfig{}, true
	}

	// Parse positional or --task=<type>
	args := os.Args[1:]
	if len(args) > 0 && args[0][0] != '-' {
		taskStr = args[0]
		args = args[1:]
	}

	// Simple flag parsing (avoid needing external deps)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--task" || arg == "-T":
			if i+1 < len(args) {
				i++
				taskStr = args[i]
			}
		case arg == "--metadata-date" || arg == "-m":
			if i+1 < len(args) {
				i++
				metadataDate = args[i]
			}
		case arg == "--output-date" || arg == "-o":
			if i+1 < len(args) {
				i++
				outputDate = args[i]
			}
		case arg == "--output-dir":
			if i+1 < len(args) {
				i++
				outputDir = args[i]
			}
		case arg == "--old-date" || arg == "-a":
			if i+1 < len(args) {
				i++
				oldDate = args[i]
			}
		case arg == "--new-date" || arg == "-b":
			if i+1 < len(args) {
				i++
				newDate = args[i]
			}
		case arg == "--concurrency" || arg == "-c":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &concurrency)
			}
		case arg == "--retry" || arg == "-r":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &retry)
			}
		case arg == "--timeout" || arg == "-w":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &timeout)
			}
		case arg == "--simple-url" || arg == "-s":
			if i+1 < len(args) {
				i++
				simpleURL = args[i]
			}
		default:
			if taskStr == "" {
				taskStr = arg
			}
		}
	}

	if taskStr == "" {
		fmt.Fprintln(os.Stderr, "Error: no task specified")
		printUsage()
		os.Exit(1)
	}

	// Load defaults, then apply overrides
	cfg := config.DefaultConfig()

	switch taskStr {
	case "metadata-sync", "meta":
		cfg.SelectedTask = types.PypiTaskMetadataSync
		if metadataDate != "" {
			cfg.PyPI.MetadataSync.SnapshotDate = metadataDate
		}
	case "artifact-download", "artifact":
		if outputDate != "" {
			fmt.Fprintln(os.Stderr, "Error: --output-date/-o is only for incremental-download; use --output-dir for artifact-download")
			os.Exit(1)
		}
		cfg.SelectedTask = types.PypiTaskArtifactDownload
		if metadataDate != "" {
			cfg.PyPI.ArtifactDownload.MetadataDate = metadataDate
		}
		if outputDir != "" {
			cfg.PyPI.ArtifactDownload.OutputDir = outputDir
		} else if metadataDate != "" {
			// Default output dir matches the snapshot directory name.
			cfg.PyPI.ArtifactDownload.OutputDir = config.FallbackOutputDir(metadataDate, "")
		}
	case "incremental-download", "incremental":
		cfg.SelectedTask = types.PypiTaskIncrementalDownload
		if oldDate != "" {
			cfg.PyPI.IncrementalDownload.OldMetadataDate = oldDate
		}
		if newDate != "" {
			cfg.PyPI.IncrementalDownload.NewMetadataDate = newDate
		}
		if outputDate != "" {
			cfg.PyPI.IncrementalDownload.OutputDate = outputDate
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown task %q\n", taskStr)
		printUsage()
		os.Exit(1)
	}

	if simpleURL != "" {
		cfg.Base.SimpleURL = simpleURL
	}
	if concurrency > 0 {
		cfg.Base.Concurrency = concurrency
	}
	if retry >= 0 {
		cfg.Base.Retry = retry
	}
	if timeout > 0 {
		cfg.Base.TimeoutMs = timeout
	}

	return cfg, false
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: mirror-sync [--tui | <task> [options]]

Tasks:
  metadata-sync     下载元数据快照
  artifact-download 按元数据快照下载包
  incremental-download 增量下载（比较两个快照）

Task-specific options:
  --metadata-date, -m  指定元数据日期 (YYYY-MM-DD)
  --output-dir         指定输出目录名 (默认: pypi-<元数据日期>)
  --output-date, -o    指定输出日期 (YYYY-MM-DD, 仅 incremental 使用)
  --old-date, -a       旧元数据日期 (for incremental)
  --new-date, -b       新元数据日期 (for incremental)

Global options:
  --concurrency, -c  并发数 (default: 16)
  --retry, -r        重试次数 (default: 2)
  --timeout, -w      超时毫秒 (default: 60000)
  --simple-url, -s   PyPI simple URL
  --tui, -t          启动交互式 TUI (默认)
  --help, -h         显示帮助

Examples:
  mirror-sync                                          # 启动 TUI
  mirror-sync metadata-sync                            # 下载今日元数据
  mirror-sync meta -m 2025-07-25                       # 下载指定日期的元数据
  mirror-sync artifact -m 2025-07-25                        # 按元数据下载包 (输出到 pypi-2025-07-25)
  mirror-sync artifact -m 2025-07-25 --output-dir pypi-mirror # 按元数据下载包到自定义目录
  mirror-sync incremental -a 2025-07-24 -b 2025-07-25 -o 2025-07-25
`)
}

func runCLI(cfg types.AppConfig) error {
	fmt.Printf("Provider: %s\n", cfg.Base.Provider)
	fmt.Printf("Task: %s\n", config.TaskLabel(cfg.SelectedTask))
	fmt.Printf("Simple URL: %s\n", cfg.Base.SimpleURL)
	fmt.Printf("Metadata Root: %s\n", cfg.Base.MetadataRoot)
	fmt.Printf("Mirror Root: %s\n", cfg.Base.MirrorRoot)
	fmt.Printf("Concurrency: %d\n", cfg.Base.Concurrency)
	fmt.Printf("Retry: %d\n", cfg.Base.Retry)
	fmt.Println()

	result, err := runner.RunSync(runner.RunSyncOptions{
		Config: cfg,
		OnEvent: func(ev runner.SyncEvent) {
			if ev.Progress != nil {
				pct := 0
				if ev.Progress.Total > 0 {
					pct = ev.Progress.Current * 100 / ev.Progress.Total
				}
				fmt.Printf("\r[%s] %s [%d/%d] %d%%", ev.Stage, ev.Message, ev.Progress.Current, ev.Progress.Total, pct)
				if ev.Progress.Failed > 0 {
					fmt.Printf(" (failed: %d)", ev.Progress.Failed)
				}
			} else {
				fmt.Printf("[%s] %s\n", ev.Stage, ev.Message)
			}
		},
	})
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== Summary ===")
	fmt.Printf("  Provider: %s\n", result.Provider)
	fmt.Printf("  Task: %s\n", config.TaskLabel(result.TaskType))
	if result.SnapshotID != nil {
		fmt.Printf("  Snapshot: %s\n", *result.SnapshotID)
	}
	if result.SnapshotRoot != nil {
		fmt.Printf("  Snapshot Root: %s\n", *result.SnapshotRoot)
	}
	if result.PackageCount != nil {
		fmt.Printf("  Packages: %d\n", *result.PackageCount)
	}
	if result.Plan != nil {
		fmt.Printf("  Plan Entries: %d\n", len(result.Plan.Entries))
		fmt.Printf("  Skipped Existing: %d\n", len(result.Plan.SkippedExisting))
	}
	if result.Diff != nil {
		fmt.Printf("  Added: %d, Changed: %d\n", len(result.Diff.Added), len(result.Diff.Changed))
	}
	if result.DownloadSummary != nil {
		fmt.Printf("  Downloaded: %d\n", result.DownloadSummary.Downloaded)
		fmt.Printf("  Attempted: %d\n", result.DownloadSummary.Attempted)
		fmt.Printf("  Skipped: %d\n", result.DownloadSummary.Skipped)
		if len(result.DownloadSummary.Failed) > 0 {
			fmt.Printf("  Failed: %d\n", len(result.DownloadSummary.Failed))
			for _, f := range result.DownloadSummary.Failed {
				fmt.Printf("    - %s: %s\n", f.Entry.RelativePath, f.Error)
			}
		}
	}
	if result.OutputRoot != nil {
		fmt.Printf("  Output Root: %s\n", *result.OutputRoot)
	}

	return nil
}
