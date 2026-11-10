package server

// User-visible strings the server sends (ARCHITECTURE §7).
const (
	msgReadOnly    = "This outline is read-only; nothing was changed"
	msgSaveFailed  = "Couldn't save: "
	msgNoSession   = "Run hyphal to open this outline."
	msgUnknownName = "unknown action: "
)
