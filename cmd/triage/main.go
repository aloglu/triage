package main

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aloglu/triage/internal/app"
)

func main() {
	p := tea.NewProgram(app.New(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
