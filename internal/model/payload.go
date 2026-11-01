package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// InsertFields is the after payload of an insert op (CONTRACTS §5). Its before
// payload is null.
type InsertFields struct {
	ParentID    NodeID `json:"parent_id"`
	Position    string `json:"position"`
	Text        string `json:"text"`
	IsTask      bool   `json:"is_task"`
	IsHeading   bool   `json:"is_heading"`
	CompletedAt *Time  `json:"completed_at"`
	CreatedAt   Time   `json:"created_at"`
}

// DeleteFields is both payloads of delete and restore ops (CONTRACTS §5).
// MergedInto is present only on a delete made by a merge (§5.3) and on the
// before of the restore that undoes it.
type DeleteFields struct {
	Deleted    bool         `json:"deleted"`
	DeletedAt  *Time        `json:"deleted_at"`
	DeletedBy  *ChangeSetID `json:"deleted_by"`
	MergedInto *NodeID      `json:"merged_into,omitempty"`
}

// TextFields is both payloads of an edit_text op (CONTRACTS §5).
type TextFields struct {
	Text string `json:"text"`
}

// PageFields is the payload of an edit_page op; null stands for no page, in
// before when creating and in after when deleting (CONTRACTS §5).
type PageFields struct {
	Markdown string `json:"markdown"`
}

// MoveFields is both payloads of a move op (CONTRACTS §5).
type MoveFields struct {
	ParentID NodeID `json:"parent_id"`
	Position string `json:"position"`
}

// TaskFields is both payloads of a set_task op (CONTRACTS §5).
type TaskFields struct {
	IsTask bool `json:"is_task"`
}

// HeadingFields is both payloads of a set_heading op (CONTRACTS §5).
type HeadingFields struct {
	IsHeading bool `json:"is_heading"`
}

// CompleteFields is both payloads of a set_complete op (CONTRACTS §5).
type CompleteFields struct {
	CompletedAt *Time `json:"completed_at"`
}

// StarFields is both payloads of a set_star op (CONTRACTS §5).
type StarFields struct {
	StarredAt *Time `json:"starred_at"`
}

var jsonNull = json.RawMessage("null")

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), jsonNull)
}

// slot describes one side of an op kind: a pointer to a new payload struct
// (nil if the side is always null) and whether null is allowed.
func slot(kind OpKind, after bool) (ptr any, nullable bool, err error) {
	switch kind {
	case OpInsert:
		if after {
			return new(InsertFields), false, nil
		}
		return nil, true, nil
	case OpDelete, OpRestore:
		return new(DeleteFields), false, nil
	case OpEditText:
		return new(TextFields), false, nil
	case OpEditPage:
		return new(PageFields), true, nil
	case OpMove:
		return new(MoveFields), false, nil
	case OpSetTask:
		return new(TaskFields), false, nil
	case OpSetHeading:
		return new(HeadingFields), false, nil
	case OpSetComplete:
		return new(CompleteFields), false, nil
	case OpSetStar:
		return new(StarFields), false, nil
	}
	return nil, false, fmt.Errorf("unknown op kind %q", kind)
}

// EncodeBefore sets op.Before from a payload struct, or null when p is nil
// (CONTRACTS §5). It rejects a payload that doesn't fit op.Kind.
func (o *Op) EncodeBefore(p any) error {
	raw, err := encode(o.Kind, false, p)
	if err != nil {
		return err
	}
	o.Before = raw
	return nil
}

// EncodeAfter sets op.After from a payload struct, or null when p is nil
// (CONTRACTS §5). It rejects a payload that doesn't fit op.Kind.
func (o *Op) EncodeAfter(p any) error {
	raw, err := encode(o.Kind, true, p)
	if err != nil {
		return err
	}
	o.After = raw
	return nil
}

func encode(kind OpKind, after bool, p any) (json.RawMessage, error) {
	ptr, nullable, err := slot(kind, after)
	if err != nil {
		return nil, err
	}
	v := reflect.ValueOf(p)
	if p == nil || (v.Kind() == reflect.Pointer && v.IsNil()) {
		if !nullable {
			return nil, fmt.Errorf("%s payload can't be null", kind)
		}
		return jsonNull, nil
	}
	v = reflect.Indirect(v)
	if ptr == nil || v.Type() != reflect.TypeOf(ptr).Elem() {
		return nil, fmt.Errorf("%s payload can't be %T", kind, p)
	}
	return json.Marshal(v.Interface())
}

// DecodeBefore returns op.Before as a pointer to its payload struct, or nil
// for null (CONTRACTS §5).
func (o Op) DecodeBefore() (any, error) {
	return decode(o.Kind, false, o.Before)
}

// DecodeAfter returns op.After as a pointer to its payload struct, or nil for
// null (CONTRACTS §5).
func (o Op) DecodeAfter() (any, error) {
	return decode(o.Kind, true, o.After)
}

func decode(kind OpKind, after bool, raw json.RawMessage) (any, error) {
	ptr, nullable, err := slot(kind, after)
	if err != nil {
		return nil, err
	}
	if isNull(raw) {
		if !nullable {
			return nil, fmt.Errorf("%s payload can't be null", kind)
		}
		return nil, nil
	}
	if ptr == nil {
		return nil, fmt.Errorf("%s payload must be null: %s", kind, raw)
	}
	if err := json.Unmarshal(raw, ptr); err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", kind, err)
	}
	if err := checkFields(raw, reflect.TypeOf(ptr).Elem()); err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", kind, err)
	}
	return ptr, nil
}

// checkFields rejects a payload with a field missing, or null where the field
// isn't nullable, since JSON decoding would read those as zero values
// (CONTRACTS §5). Only omitempty fields may be absent.
func checkFields(raw json.RawMessage, t reflect.Type) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		value, ok := fields[name]
		switch {
		case !ok && opts != "omitempty":
			return fmt.Errorf("field %s is missing", name)
		case ok && isNull(value) && f.Type.Kind() != reflect.Pointer:
			return fmt.Errorf("field %s can't be null", name)
		}
	}
	return nil
}

// Inverse returns the op that undoes op: the same kind with before and after
// swapped, except that insert inverts to delete and delete and restore invert
// to each other (CONTRACTS §5). The delete made from an insert has no time or
// change set yet; store.Commit stamps them (D-049).
func Inverse(op Op) Op {
	inv := Op{NodeID: op.NodeID, Kind: op.Kind, Before: op.After, After: op.Before}
	switch op.Kind {
	case OpInsert:
		inv.Kind = OpDelete
		inv.Before = json.RawMessage(`{"deleted":false,"deleted_at":null,"deleted_by":null}`)
		inv.After = json.RawMessage(`{"deleted":true,"deleted_at":null,"deleted_by":null}`)
	case OpDelete:
		inv.Kind = OpRestore
	case OpRestore:
		inv.Kind = OpDelete
	}
	return inv
}

// Current holds the present values an op's fields are compared with. A nil
// Node or Page means the row doesn't exist; a nil StarredAt means no star.
type Current struct {
	Node      *Node
	Page      *Page
	StarredAt *Time
}

// ChangedFields returns the fields of op.After whose current value differs,
// named as in the payload. Revert and undo skip these fields (§8.2, CONTRACTS
// §5). A missing node differs in every field. merged_into is not compared,
// since it isn't stored on the node.
func ChangedFields(op Op, cur Current) ([]string, error) {
	p, err := op.DecodeAfter()
	if err != nil {
		return nil, err
	}
	var node Node
	if cur.Node != nil {
		node = *cur.Node
	}
	var changed []string
	check := func(field string, same bool) {
		if cur.Node == nil || !same {
			changed = append(changed, field)
		}
	}
	switch a := p.(type) {
	case *InsertFields:
		check("parent_id", a.ParentID == node.ParentID)
		check("position", a.Position == node.Position)
		check("text", a.Text == node.Text)
		check("is_task", a.IsTask == node.IsTask)
		check("is_heading", a.IsHeading == node.IsHeading)
		check("completed_at", sameTime(a.CompletedAt, node.CompletedAt))
		check("created_at", a.CreatedAt.String() == node.CreatedAt.String())
	case *DeleteFields:
		deletedBy := ChangeSetID("")
		if a.DeletedBy != nil {
			deletedBy = *a.DeletedBy
		}
		check("deleted", a.Deleted == (node.DeletedAt != nil))
		check("deleted_at", sameTime(a.DeletedAt, node.DeletedAt))
		check("deleted_by", deletedBy == node.DeletedBy)
	case *TextFields:
		check("text", a.Text == node.Text)
	case *MoveFields:
		check("parent_id", a.ParentID == node.ParentID)
		check("position", a.Position == node.Position)
	case *TaskFields:
		check("is_task", a.IsTask == node.IsTask)
	case *HeadingFields:
		check("is_heading", a.IsHeading == node.IsHeading)
	case *CompleteFields:
		check("completed_at", sameTime(a.CompletedAt, node.CompletedAt))
	case *StarFields:
		if !sameTime(a.StarredAt, cur.StarredAt) {
			changed = append(changed, "starred_at")
		}
	case *PageFields:
		if cur.Page == nil || cur.Page.Markdown != a.Markdown {
			changed = append(changed, "markdown")
		}
	case nil: // edit_page that deleted the page
		if cur.Page != nil {
			changed = append(changed, "markdown")
		}
	}
	return changed, nil
}

func sameTime(a, b *Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.String() == b.String()
}
