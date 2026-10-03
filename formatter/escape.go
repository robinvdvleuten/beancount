package formatter

// StringEscapeStyle was how the formatter escaped the strings it printed.
//
// Deprecated: the formatter copies every string from the source, so the
// style has no effect.
type StringEscapeStyle int

// The styles are kept for callers that name them; none has an effect.
const (
	// EscapeStyleNone outputs strings without escape sequences.
	// Newlines, tabs, quotes become their literal characters (multi-line output).
	EscapeStyleNone StringEscapeStyle = iota
	// EscapeStyleCStyle outputs strings with C-style escape sequences.
	// Newlines become \n, tabs become \t, quotes become \", backslashes become \\.
	EscapeStyleCStyle
	// EscapeStyleOriginal tries to match the original source escape style.
	// Falls back to CStyle if original raw token is unavailable.
	EscapeStyleOriginal
)
