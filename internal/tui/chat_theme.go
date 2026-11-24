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
	chatAccentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorAccent))
	chatWarnStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorWarning))
	chatBlueStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorBlue))
	chatBrightStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorBrightText))
	welcomeCardStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection)).Padding(0, 2)
	chatHeaderStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorText)).Background(lipgloss.Color(theme.ColorPanel)).Bold(true)
	statusBarStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSubtleText)).Background(lipgloss.Color(theme.ColorSurface))
	inputBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection))
	// Precomputed input box border variants: deriving them per frame allocates
	// a new style on every render.
	inputBoxReadyStyle = inputBoxStyle.BorderForeground(lipgloss.Color(theme.ColorSuccess))
	inputBoxBusyStyle  = inputBoxStyle.BorderForeground(lipgloss.Color(theme.ColorPrimary))
	inputBoxErrorStyle = inputBoxStyle.BorderForeground(lipgloss.Color(theme.ColorDanger))
	popupStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(theme.ColorSelection)).Padding(0, 2)
	popupSelStyle      = lipgloss.NewStyle().Background(lipgloss.Color(theme.ColorSelection)).Foreground(lipgloss.Color(theme.ColorText)).Bold(true)
	popupNormalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorSoftText))
)
