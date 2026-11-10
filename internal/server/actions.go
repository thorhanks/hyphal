package server

// Internal action names, besides the keymap command IDs (CONTRACTS §12.1).
// P0-18 handles only these; the keymap commands arrive with their cards.
const (
	// actSetCursor moves this window's cursor to {id, off} (§6.1, §6.5).
	actSetCursor = "set_cursor"
)
