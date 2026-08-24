package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/uninstall"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "paths":
			if len(args) != 1 {
				return fmt.Errorf("usage: triage paths")
			}
			return uninstall.PrintPaths(os.Stdout)
		case "uninstall":
			return uninstall.Run(args[1:], os.Stdin, os.Stdout, os.Stderr)
		case "help", "--help", "-h":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown command %q; run triage --help for usage", args[0])
		}
	}

	p := tea.NewProgram(app.New(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func printUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  triage                    start the app")
	fmt.Fprintln(os.Stdout, "  triage paths              show local application paths")
	fmt.Fprintln(os.Stdout, "  triage uninstall          interactively remove triage")
	fmt.Fprintln(os.Stdout, "  triage uninstall --help   show uninstall options")
}
