package model

import "encoding/json"

// Node is one row of the outline tree (§7.2).
type Node struct {
	ID          NodeID
	ParentID    NodeID // "" for the root
	Position    string // fractional key (CONTRACTS §3)
	Text        string // one line of stored markup (§5.8), no leading marker (§5.4)
	IsTask      bool
	IsHeading   bool
	CompletedAt *Time
	CreatedAt   Time
	UpdatedAt   Time
	Version     int64
	DeletedAt   *Time
	DeletedBy   ChangeSetID // "" when live
}

// Page is the optional Markdown page of a bullet (§5.7, §7.2).
type Page struct {
	NodeID    NodeID
	Markdown  string
	UpdatedAt Time
	Version   int64
}

// OpKind names the kind of change an Op records (§8.2).
type OpKind string

// The op kinds (§8.2, CONTRACTS §5).
const (
	OpInsert      OpKind = "insert"
	OpDelete      OpKind = "delete"
	OpRestore     OpKind = "restore"
	OpEditText    OpKind = "edit_text"
	OpEditPage    OpKind = "edit_page"
	OpMove        OpKind = "move"
	OpSetTask     OpKind = "set_task"
	OpSetHeading  OpKind = "set_heading"
	OpSetComplete OpKind = "set_complete"
	OpSetStar     OpKind = "set_star"
)

// Op is one recorded change to one node; Before and After hold the payloads
// in CONTRACTS §5 (§8.2).
type Op struct {
	NodeID NodeID
	Kind   OpKind
	Before json.RawMessage
	After  json.RawMessage
}

// ChangeSet groups the ops of one action, the unit of undo and revert (§8.2).
type ChangeSet struct {
	ID          ChangeSetID
	At          Time
	Source      string // "ui", "cli", "import", "restore" or "agent:<name>"
	Description string
	UndoOf      ChangeSetID // "" if none
	Ops         []Op
}
