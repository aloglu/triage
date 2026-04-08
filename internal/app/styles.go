package app

import "github.com/charmbracelet/lipgloss"

type styles struct {
	app                     lipgloss.Style
	panel                   lipgloss.Style
	panelMuted              lipgloss.Style
	panelFocused            lipgloss.Style
	title                   lipgloss.Style
	subtitle                lipgloss.Style
	muted                   lipgloss.Style
	selected                lipgloss.Style
	selectedMuted           lipgloss.Style
	label                   lipgloss.Style
	labelMuted              lipgloss.Style
	stageIdea               lipgloss.Style
	stagePlanned            lipgloss.Style
	stageActive             lipgloss.Style
	stageBlocked            lipgloss.Style
	stageDone               lipgloss.Style
	stageIdeaText           lipgloss.Style
	stagePlannedText        lipgloss.Style
	stageActiveText         lipgloss.Style
	stageBlockedText        lipgloss.Style
	stageDoneText           lipgloss.Style
	help                    lipgloss.Style
	statusInfo              lipgloss.Style
	statusSuccess           lipgloss.Style
	statusWarning           lipgloss.Style
	statusError             lipgloss.Style
	statusLoading           lipgloss.Style
	divider                 lipgloss.Style
	scrollTrack             lipgloss.Style
	scrollThumb             lipgloss.Style
	editLabel               lipgloss.Style
	editLabelActive         lipgloss.Style
	editValue               lipgloss.Style
	editHint                lipgloss.Style
	shortcutKey             lipgloss.Style
	shortcutDesc            lipgloss.Style
	footerKey               lipgloss.Style
	footerText              lipgloss.Style
	footerSeparator         lipgloss.Style
	commandGhost            lipgloss.Style
	commandMenuBox          lipgloss.Style
	commandMenuItem         lipgloss.Style
	commandMenuSelected     lipgloss.Style
	conflictChanged         lipgloss.Style
	conflictLocal           lipgloss.Style
	conflictRemote          lipgloss.Style
	conflictRemoteButton    lipgloss.Style
	conflictOverwriteButton lipgloss.Style
	confirmDangerButton     lipgloss.Style
	confirmCancelButton     lipgloss.Style
}

func newStyles() styles {
	border := lipgloss.RoundedBorder()

	return styles{
		app: lipgloss.NewStyle().
			Padding(1, 2, 0, 2),
		panel: lipgloss.NewStyle().
			Border(border).
			BorderForeground(lipgloss.Color("240")).
			Padding(1),
		panelMuted: lipgloss.NewStyle().
			Border(border).
			BorderForeground(lipgloss.Color("236")).
			Padding(1),
		panelFocused: lipgloss.NewStyle().
			Border(border).
			BorderForeground(lipgloss.Color("73")).
			Padding(1),
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")),
		subtitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("109")),
		muted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")),
		selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		selectedMuted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		label: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("239")).
			Padding(0, 1),
		labelMuted: lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Background(lipgloss.Color("236")).
			Padding(0, 1),
		stageIdea: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("57")).
			Padding(0, 1),
		stagePlanned: lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("150")).
			Padding(0, 1),
		stageActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("110")).
			Padding(0, 1),
		stageBlocked: lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("215")).
			Padding(0, 1),
		stageDone: lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("108")).
			Padding(0, 1),
		stageIdeaText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("111")),
		stagePlannedText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("150")),
		stageActiveText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("110")),
		stageBlockedText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("215")),
		stageDoneText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("108")),
		help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")),
		statusInfo: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		statusSuccess: lipgloss.NewStyle().
			Foreground(lipgloss.Color("114")).
			Bold(true),
		statusWarning: lipgloss.NewStyle().
			Foreground(lipgloss.Color("221")).
			Bold(true),
		statusError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		statusLoading: lipgloss.NewStyle().
			Foreground(lipgloss.Color("117")).
			Bold(true),
		divider: lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")),
		scrollTrack: lipgloss.NewStyle().
			Foreground(lipgloss.Color("236")),
		scrollThumb: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")),
		editLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color("246")),
		editLabelActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		editValue: lipgloss.NewStyle().
			PaddingLeft(0),
		editHint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			PaddingLeft(10),
		shortcutKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		shortcutDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")),
		footerKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		footerText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")),
		footerSeparator: lipgloss.NewStyle().
			Foreground(lipgloss.Color("239")),
		commandGhost: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")),
		commandMenuBox: lipgloss.NewStyle().
			Border(border).
			BorderForeground(lipgloss.Color("239")).
			Padding(0, 1),
		commandMenuItem: lipgloss.NewStyle().
			Foreground(lipgloss.Color("247")),
		commandMenuSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true),
		conflictChanged: lipgloss.NewStyle().
			Foreground(lipgloss.Color("221")).
			Bold(true),
		conflictLocal: lipgloss.NewStyle().
			Foreground(lipgloss.Color("114")).
			Bold(true),
		conflictRemote: lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true),
		conflictRemoteButton: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("160")).
			Bold(true).
			Padding(0, 1),
		conflictOverwriteButton: lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("71")).
			Bold(true).
			Padding(0, 1),
		confirmDangerButton: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("160")).
			Bold(true).
			Padding(0, 1),
		confirmCancelButton: lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("245")).
			Bold(true).
			Padding(0, 1),
	}
}
