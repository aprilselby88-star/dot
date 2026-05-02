package tui

import "github.com/charmbracelet/lipgloss"

// ---- palette (Catppuccin Mocha-inspired) ----
var (
	colorSurface  = lipgloss.Color("236") // subtle selection bg
	colorOverlay  = lipgloss.Color("238") // borders, dividers
	colorMuted    = lipgloss.Color("244") // secondary text
	colorSubtle   = lipgloss.Color("240") // very muted — dividers, separators
	colorText     = lipgloss.Color("253") // primary text
	colorIris     = lipgloss.Color("141") // purple — primary accent
	colorFoam     = lipgloss.Color("80")  // teal — keys, @mentions
	colorGold     = lipgloss.Color("214") // amber — carried items, warnings
	colorPine     = lipgloss.Color("114") // green — success, done
	colorLove     = lipgloss.Color("210") // rose — errors

	// ---- tab bar ----

	// Active tab: purple pill (bg colour, black text)
	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(colorIris).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Padding(0, 2)

	// ---- list rows ----

	// Full-width background highlight for selected rows. Caller must set Width().
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Background(colorSurface).
			Foreground(colorIris)

	doneStyle = lipgloss.NewStyle().
			Foreground(colorSubtle).
			Strikethrough(true)

	carriedStyle = lipgloss.NewStyle().
			Foreground(colorGold).
			Italic(true)

	tagStyle = lipgloss.NewStyle().
			Foreground(colorPine).
			Italic(true)

	// ---- typography ----

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	subtleStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	// ---- status bar / hints ----

	keyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFoam)

	hintStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPine)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLove)

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(colorMuted).
			Padding(0, 1)

	// ---- inputs ----

	inputLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorIris)

	// Unfocused border
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorOverlay).
			Padding(0, 1)

	// Focused border (iris)
	inputBoxFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorIris).
				Padding(0, 1)

	// ---- section headers ----

	sectionHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorIris)

	// ---- GitHub / PR state ----

	prOpenStyle   = lipgloss.NewStyle().Foreground(colorPine)
	prMergedStyle = lipgloss.NewStyle().Foreground(colorIris)
	prClosedStyle = lipgloss.NewStyle().Foreground(colorLove)

	// ---- @mentions ----

	mentionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFoam)
)

func prStateStyle(state string) lipgloss.Style {
	switch state {
	case "open":
		return prOpenStyle
	case "merged":
		return prMergedStyle
	case "closed":
		return prClosedStyle
	default:
		return mutedStyle
	}
}
