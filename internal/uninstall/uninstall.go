// Package uninstall removes triage and its local files.
package uninstall

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/store"
)

type targetKind string

const (
	targetBinary targetKind = "executable"
	targetConfig targetKind = "configuration"
	targetCache  targetKind = "cache"
	targetSource targetKind = "Go download"
)

// modulePath is triage's Go module, whose downloaded copies `go install`
// leaves in the module cache.
const modulePath = "github.com/aloglu/triage"

type target struct {
	kind      targetKind
	path      string
	recursive bool
}

// Plan lists what an uninstall removes.
type Plan struct {
	Executable string
	Paths      config.Paths
	KeepData   bool
	// Unsent counts changes in the outbox that would be lost.
	Unsent  int
	targets []target
}

type options struct {
	dryRun   bool
	keepData bool
	yes      bool
}

var executablePath = os.Executable

// goModCache returns Go's module cache directory, or "" when Go isn't
// installed. Tests replace it.
var goModCache = func() string {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sourceTargets finds the copies of triage's source that `go install`
// downloaded. Libraries triage used are left alone: other Go programs may
// share them.
func sourceTargets() []target {
	cache := goModCache()
	if cache == "" {
		return nil
	}
	var targets []target
	owner, name := filepath.Split(filepath.FromSlash(modulePath))
	matches, _ := filepath.Glob(filepath.Join(cache, owner, name+"@*"))
	for _, match := range matches {
		targets = append(targets, target{kind: targetSource, path: match, recursive: true})
	}
	download := filepath.Join(cache, "cache", "download", filepath.FromSlash(modulePath))
	if _, err := os.Stat(download); err == nil {
		targets = append(targets, target{kind: targetSource, path: download, recursive: true})
	}
	return targets
}

// PrintPaths writes the locations triage uses.
func PrintPaths(out io.Writer) error {
	plan, err := discover(false)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Executable:     %s\n", plan.Executable)
	fmt.Fprintf(out, "Configuration:  %s\n", plan.Paths.ConfigFile())
	fmt.Fprintf(out, "Unsent changes: %s\n", plan.Paths.OutboxDir())
	fmt.Fprintf(out, "Cache:          %s\n", plan.Paths.CacheDir)
	return nil
}

// Run implements `triage uninstall`.
func Run(args []string, in io.Reader, out, errOut io.Writer) error {
	opts, err := parseOptions(args, errOut)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	plan, err := discover(opts.keepData)
	if err != nil {
		return err
	}
	printPlan(out, plan, opts.dryRun)
	if opts.dryRun {
		return nil
	}

	if !opts.yes {
		confirmed, err := confirm(in, out)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(out, "Uninstall cancelled.")
			return nil
		}
	}

	return execute(plan, out)
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("triage uninstall", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&opts.dryRun, "dry-run", false, "show what would be removed without deleting anything")
	flags.BoolVar(&opts.keepData, "keep-data", false, "remove the executable but keep configuration and cache")
	flags.BoolVar(&opts.yes, "yes", false, "skip the confirmation prompt")
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: triage uninstall [--dry-run] [--keep-data] [--yes]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	return opts, nil
}

func discover(keepData bool) (Plan, error) {
	executable, err := executablePath()
	if err != nil {
		return Plan{}, fmt.Errorf("resolve executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve executable path: %w", err)
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		Executable: filepath.Clean(executable),
		Paths:      paths,
		KeepData:   keepData,
		targets:    []target{{kind: targetBinary, path: filepath.Clean(executable)}},
	}
	plan.targets = append(plan.targets, sourceTargets()...)
	if keepData {
		return plan, nil
	}
	if ops, err := store.New(paths).Pending(); err == nil {
		plan.Unsent = len(ops)
	}
	plan.targets = append(plan.targets,
		target{kind: targetConfig, path: filepath.Clean(paths.ConfigDir), recursive: true},
		target{kind: targetCache, path: filepath.Clean(paths.CacheDir), recursive: true},
	)
	return plan, nil
}

func printPlan(out io.Writer, plan Plan, dryRun bool) {
	if dryRun {
		fmt.Fprintln(out, "Uninstall preview (nothing will be removed):")
	} else {
		fmt.Fprintln(out, "The following local paths will be permanently removed:")
	}
	for _, target := range plan.targets {
		fmt.Fprintf(out, "  %-15s %s\n", string(target.kind)+":", target.path)
	}
	if plan.KeepData {
		fmt.Fprintln(out, "Configuration and cache will be kept.")
	}
	fmt.Fprintln(out, "Libraries Go downloaded for triage stay, since other Go programs may use them; `go clean -modcache` clears them all.")
	if plan.Unsent > 0 {
		fmt.Fprintf(out, "Warning: %d change(s) haven't been sent to GitHub yet and will be lost.\n", plan.Unsent)
		fmt.Fprintln(out, "Open triage while online to send them first.")
	}
	fmt.Fprintln(out, "Issues and labels on GitHub will not be changed.")
}

func confirm(in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "Continue? [y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func execute(plan Plan, out io.Writer) error {
	for _, target := range plan.targets {
		switch {
		case target.kind == targetSource:
			if err := validateSourceTarget(target.path); err != nil {
				return err
			}
		case target.recursive:
			if err := validateRecursiveTarget(target.path); err != nil {
				return err
			}
		}
	}

	for _, target := range plan.targets {
		if target.kind == targetSource {
			// Go makes downloaded sources read-only.
			makeWritable(target.path)
		}
		if target.recursive {
			if err := os.RemoveAll(target.path); err != nil {
				return fmt.Errorf("remove %s %s: %w", target.kind, target.path, err)
			}
		} else if err := os.Remove(target.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s %s: %w", target.kind, target.path, err)
		}
		fmt.Fprintf(out, "Removed %s: %s\n", target.kind, target.path)
	}
	fmt.Fprintln(out, "triage has been uninstalled from this system.")
	return nil
}

// validateRecursiveTarget refuses to delete directories that obviously
// aren't triage's own, in case a path override points somewhere unexpected.
func validateRecursiveTarget(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || path == filepath.VolumeName(path)+string(filepath.Separator) {
		return fmt.Errorf("refusing to recursively remove unsafe path %q", path)
	}
	if filepath.Base(path) != "triage" {
		return fmt.Errorf("refusing to recursively remove %s: not a triage directory", path)
	}
	home, _ := os.UserHomeDir()
	if home != "" && samePath(path, home) {
		return fmt.Errorf("refusing to recursively remove home directory %s", path)
	}
	return nil
}

// validateSourceTarget only allows triage's own folders inside Go's module
// cache.
func validateSourceTarget(path string) error {
	cache := goModCache()
	rel, err := filepath.Rel(cache, path)
	if cache == "" || err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to remove %s: not in Go's module cache", path)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, modulePath+"@") && rel != "cache/download/"+modulePath {
		return fmt.Errorf("refusing to remove %s: not triage's source", path)
	}
	return nil
}

func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil {
			_ = os.Chmod(path, info.Mode().Perm()|0o200)
		}
		return nil
	})
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}
