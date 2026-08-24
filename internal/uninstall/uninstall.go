package uninstall

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aloglu/triage/internal/config"
)

type targetKind string

const (
	targetBinary    targetKind = "binary"
	targetAppData   targetKind = "application data"
	targetDataFile  targetKind = "custom data file"
	targetDraftsDir targetKind = "custom drafts folder"
)

type target struct {
	kind      targetKind
	path      string
	recursive bool
}

type Plan struct {
	Executable   string
	ConfigFile   string
	DataFile     string
	DraftsFolder string
	KeepData     bool
	targets      []target
}

type options struct {
	dryRun   bool
	keepData bool
	yes      bool
}

var executablePath = os.Executable

func PrintPaths(out io.Writer) error {
	plan, err := discover(false)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Executable:    %s\n", plan.Executable)
	fmt.Fprintf(out, "Configuration: %s\n", plan.ConfigFile)
	fmt.Fprintf(out, "Local data:    %s\n", plan.DataFile)
	fmt.Fprintf(out, "Drafts:       %s\n", plan.DraftsFolder)
	return nil
}

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
	flags.BoolVar(&opts.keepData, "keep-data", false, "remove the executable but preserve local data and configuration")
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

	manager, err := config.NewManager()
	if err != nil {
		return Plan{}, fmt.Errorf("resolve configuration: %w", err)
	}
	configFile := manager.Path()
	appDir := filepath.Dir(configFile)
	plan := Plan{
		Executable: filepath.Clean(executable),
		ConfigFile: filepath.Clean(configFile),
		KeepData:   keepData,
		targets:    []target{{kind: targetBinary, path: filepath.Clean(executable)}},
	}
	if keepData {
		return plan, nil
	}

	cfg, ok, err := manager.Load()
	if err != nil {
		return Plan{}, fmt.Errorf("load %s before uninstalling: %w", configFile, err)
	}
	if !ok {
		dataFile, dataErr := config.DefaultDataFile()
		if dataErr != nil {
			return Plan{}, fmt.Errorf("resolve default data file: %w", dataErr)
		}
		draftsFolder, draftsErr := config.DefaultDraftsFolder()
		if draftsErr != nil {
			return Plan{}, fmt.Errorf("resolve default drafts folder: %w", draftsErr)
		}
		cfg.DataFile = dataFile
		cfg.DraftsFolder = draftsFolder
	}

	plan.DataFile, err = absolutePath(cfg.DataFile)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve data file: %w", err)
	}
	plan.DraftsFolder, err = absolutePath(cfg.DraftsFolder)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve drafts folder: %w", err)
	}

	if !within(plan.DataFile, appDir) {
		plan.targets = append(plan.targets, target{kind: targetDataFile, path: plan.DataFile})
	}
	if !within(plan.DraftsFolder, appDir) {
		plan.targets = append(plan.targets, target{kind: targetDraftsDir, path: plan.DraftsFolder, recursive: true})
	}
	plan.targets = append(plan.targets, target{kind: targetAppData, path: filepath.Clean(appDir), recursive: true})
	return plan, nil
}

func absolutePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func printPlan(out io.Writer, plan Plan, dryRun bool) {
	if dryRun {
		fmt.Fprintln(out, "Uninstall preview (nothing will be removed):")
	} else {
		fmt.Fprintln(out, "The following local paths will be permanently removed:")
	}
	for _, target := range plan.targets {
		fmt.Fprintf(out, "  %-20s %s\n", string(target.kind)+":", target.path)
	}
	if plan.KeepData {
		fmt.Fprintln(out, "Local configuration, items, and drafts will be kept.")
	}
	fmt.Fprintln(out, "Synced GitHub issues and repository labels will not be changed.")
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
		if target.recursive {
			if err := validateRecursiveTarget(target.path, target.kind == targetAppData); err != nil {
				return err
			}
		}
	}

	binaryRemovalPending := false
	for _, target := range plan.targets {
		if target.recursive {
			if err := os.RemoveAll(target.path); err != nil {
				return fmt.Errorf("remove %s %s: %w", target.kind, target.path, err)
			}
		} else if err := os.Remove(target.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if target.kind == targetBinary && runtime.GOOS == "windows" {
				fmt.Fprintf(out, "Could not remove the running executable. After this command exits, remove it manually:\n  Remove-Item -LiteralPath %s\n", powershellQuote(target.path))
				binaryRemovalPending = true
				continue
			}
			return fmt.Errorf("remove %s %s: %w", target.kind, target.path, err)
		}
		fmt.Fprintf(out, "Removed %s: %s\n", target.kind, target.path)
	}
	if binaryRemovalPending {
		if plan.KeepData {
			fmt.Fprintln(out, "Local data was kept. Run the command above to finish uninstalling triage.")
		} else {
			fmt.Fprintln(out, "Local data was removed. Run the command above to finish uninstalling triage.")
		}
		return nil
	}
	fmt.Fprintln(out, "triage has been uninstalled from this system.")
	return nil
}

func validateRecursiveTarget(path string, allowAppDir bool) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || path == filepath.VolumeName(path)+string(filepath.Separator) {
		return fmt.Errorf("refusing to recursively remove unsafe path %q", path)
	}
	home, _ := os.UserHomeDir()
	if home != "" && samePath(path, home) {
		return fmt.Errorf("refusing to recursively remove home directory %s", path)
	}
	configDir, _ := os.UserConfigDir()
	if !allowAppDir && configDir != "" && samePath(path, configDir) {
		return fmt.Errorf("refusing to recursively remove configuration root %s", path)
	}
	return nil
}

func within(path, parent string) bool {
	path = filepath.Clean(path)
	parent = filepath.Clean(parent)
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
