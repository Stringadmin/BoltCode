package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/Stringadmin/BoltCode/internal/theme"
)

var (
	chatUserStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorUser))
	chatAssistantStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorText)).Background(lipgloss.Color(theme.ColorPanel))
	chatDimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorMutedText))
	chatGreenStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSuccess))
	chatErrorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorDanger))
	chatPromptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorPrimary))
	chatTitleStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.ColorTitle))
	chatHeaderStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorText)).Background(lipgloss.Color(theme.ColorPanel)).Bold(true)
	statusBarStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSubtleText)).Background(lipgloss.Color(theme.ColorSurface))
	inputBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection))
	popupStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection)).Padding(0, 2)
	popupSelStyle      = lipgloss.NewStyle().Background(lipgloss.Color(theme.ColorSelection)).Foreground(lipgloss.Color(theme.ColorText)).Bold(true)
	popupNormalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSoftText))
)
