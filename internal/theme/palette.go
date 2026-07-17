// Package theme is the single color-token source for BoltCode's ANSI and
// Lipgloss terminal renderers. Consumers choose their rendering mechanism but
// share the same semantic palette.
package theme

// ── SGR codes ───────────────────────────────────────────────────────────
const (
	SGRReset     = "\033[0m"
	SGRBold      = "\033[1m"
	SGRDim       = "\033[2m"
	SGRItalic    = "\033[3m"
	SGRUnderline = "\033[4m"
)

// ── Semantic palette ─────────────────────────────────────────────────────
const (
	ColorPrimary     = "75"
	ColorSuccess     = "78"
	ColorWarning     = "220"
	ColorDanger      = "196"
	ColorText        = "255"
	ColorBrightText  = "231"
	ColorNormalText  = "252"
	ColorSoftText    = "248"
	ColorSubtleText  = "245"
	ColorMutedText   = "241"
	ColorBannerMuted = "242"
	ColorUser        = "39"
	ColorTitle       = "213"
	ColorAccent      = "141"
	ColorBlue        = "69"
	ColorOrange      = "208"
	ColorPanel       = "24"
	ColorSurface     = "236"
	ColorDeepSurface = "234"
	ColorSelection   = "63"
)

// ── ANSI escape sequences ────────────────────────────────────────────────
const (
	ANSIPrimary     = "\033[38;5;" + ColorPrimary + "m"
	ANSISuccess     = "\033[38;5;" + ColorSuccess + "m"
	ANSIWarning     = "\033[38;5;" + ColorWarning + "m"
	ANSIDanger      = "\033[38;5;" + ColorDanger + "m"
	ANSISubtleText  = "\033[38;5;" + ColorSubtleText + "m"
	ANSIMutedText   = "\033[38;5;" + ColorMutedText + "m"
	ANSIBannerMuted = "\033[38;5;" + ColorBannerMuted + "m"
	ANSIText        = "\033[97m"
	ANSIAccent      = "\033[38;5;" + ColorAccent + "m"
	ANSIBlue        = "\033[38;5;" + ColorBlue + "m"
	ANSIOrange      = "\033[38;5;" + ColorOrange + "m"
	ANSISurface     = "\033[48;5;" + ColorSurface + "m"
	ANSIPanel       = "\033[48;5;" + ColorPanel + "m"
)
