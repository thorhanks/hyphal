package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const stamp = "2026-10-17T09:30:00.123Z"

func mustTime(t *testing.T, s string) *Time {
	t.Helper()
	v, err := ParseTime(s)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func TestTime_Marshal(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"milliseconds", time.Date(2026, 10, 17, 9, 30, 0, 123e6, time.UTC), stamp},
		{"always three digits", time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC), "2026-10-17T09:30:00.000Z"},
		{"converted to UTC", time.Date(2026, 10, 17, 11, 30, 0, 123e6, time.FixedZone("", 2*3600)), stamp},
		{"truncated, not rounded", time.Date(2026, 10, 17, 9, 30, 0, 123999999, time.UTC), stamp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(NewTime(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != `"`+tt.want+`"` {
				t.Errorf("got %s, want %q", got, tt.want)
			}
			// A Time built without NewTime still marshals in the stored form.
			got, err = json.Marshal(Time{tt.in})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != `"`+tt.want+`"` {
				t.Errorf("raw Time: got %s, want %q", got, tt.want)
			}
		})
	}
}

func TestTime_MarshalRejectsYearOutOfRange(t *testing.T) {
	if _, err := json.Marshal(Time{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}); err == nil {
		t.Error("year 10000 marshalled")
	}
}

func TestTime_Parse(t *testing.T) {
	for _, in := range []string{stamp, "2026-10-17T11:30:00.123+02:00", "2026-10-17T09:30:00.123456Z"} {
		got, err := ParseTime(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got.String() != stamp {
			t.Errorf("%s: got %s", in, got)
		}
	}
	for _, in := range []string{"", "yesterday", "2026-10-17"} {
		if _, err := ParseTime(in); err == nil {
			t.Errorf("%q parsed", in)
		}
	}
	var tm Time
	if err := json.Unmarshal([]byte("123"), &tm); err == nil {
		t.Error("number parsed as time")
	}
	var p *Time
	if err := json.Unmarshal([]byte("null"), &p); err != nil || p != nil {
		t.Errorf("null: %v, %v", p, err)
	}
}

// payloadCases holds one op of every kind, with its exact JSON, nulls and all.
var payloadCases = []struct {
	name          string
	kind          OpKind
	before, after any
	beforeJ       string
	afterJ        string
}{
	{"insert", OpInsert, nil, InsertFields{ParentID: "p", Position: "a0", Text: "x", CreatedAt: Time{time.Date(2026, 10, 17, 9, 30, 0, 123e6, time.UTC)}},
		`null`, `{"parent_id":"p","position":"a0","text":"x","is_task":false,"is_heading":false,"completed_at":null,"created_at":"` + stamp + `"}`},
	{"delete", OpDelete, DeleteFields{}, DeleteFields{Deleted: true, DeletedAt: timePtr(), DeletedBy: csPtr("c")},
		`{"deleted":false,"deleted_at":null,"deleted_by":null}`, `{"deleted":true,"deleted_at":"` + stamp + `","deleted_by":"c"}`},
	{"delete by merge", OpDelete, DeleteFields{}, DeleteFields{Deleted: true, DeletedAt: timePtr(), DeletedBy: csPtr("c"), MergedInto: nodePtr("s")},
		`{"deleted":false,"deleted_at":null,"deleted_by":null}`, `{"deleted":true,"deleted_at":"` + stamp + `","deleted_by":"c","merged_into":"s"}`},
	{"restore", OpRestore, DeleteFields{Deleted: true, DeletedAt: timePtr(), DeletedBy: csPtr("c")}, DeleteFields{},
		`{"deleted":true,"deleted_at":"` + stamp + `","deleted_by":"c"}`, `{"deleted":false,"deleted_at":null,"deleted_by":null}`},
	{"edit_text", OpEditText, TextFields{"a"}, TextFields{"b"}, `{"text":"a"}`, `{"text":"b"}`},
	{"edit_page create", OpEditPage, nil, PageFields{"b"}, `null`, `{"markdown":"b"}`},
	{"edit_page delete", OpEditPage, PageFields{"a"}, nil, `{"markdown":"a"}`, `null`},
	{"move", OpMove, MoveFields{"p", "a0"}, MoveFields{"q", "a1"},
		`{"parent_id":"p","position":"a0"}`, `{"parent_id":"q","position":"a1"}`},
	{"set_task", OpSetTask, TaskFields{false}, TaskFields{true}, `{"is_task":false}`, `{"is_task":true}`},
	{"set_heading", OpSetHeading, HeadingFields{true}, HeadingFields{false}, `{"is_heading":true}`, `{"is_heading":false}`},
	{"set_complete", OpSetComplete, CompleteFields{}, CompleteFields{timePtr()}, `{"completed_at":null}`, `{"completed_at":"` + stamp + `"}`},
	{"set_star", OpSetStar, StarFields{timePtr()}, StarFields{}, `{"starred_at":"` + stamp + `"}`, `{"starred_at":null}`},
}

func timePtr() *Time {
	v, err := ParseTime(stamp)
	if err != nil {
		panic(err)
	}
	return &v
}
func csPtr(s ChangeSetID) *ChangeSetID { return &s }
func nodePtr(s NodeID) *NodeID         { return &s }

func encodedOp(t *testing.T, kind OpKind, before, after any) Op {
	t.Helper()
	op := Op{NodeID: "n", Kind: kind}
	if err := op.EncodeBefore(before); err != nil {
		t.Fatal(err)
	}
	if err := op.EncodeAfter(after); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestPayload_RoundTrip(t *testing.T) {
	for _, tt := range payloadCases {
		t.Run(tt.name, func(t *testing.T) {
			op := encodedOp(t, tt.kind, tt.before, tt.after)
			if string(op.Before) != tt.beforeJ {
				t.Errorf("before: got %s, want %s", op.Before, tt.beforeJ)
			}
			if string(op.After) != tt.afterJ {
				t.Errorf("after: got %s, want %s", op.After, tt.afterJ)
			}
			// Decoding and encoding again gives the same bytes.
			before, err := op.DecodeBefore()
			if err != nil {
				t.Fatal(err)
			}
			after, err := op.DecodeAfter()
			if err != nil {
				t.Fatal(err)
			}
			again := encodedOp(t, tt.kind, before, after)
			if !reflect.DeepEqual(op, again) {
				t.Errorf("got %s %s, want %s %s", again.Before, again.After, op.Before, op.After)
			}
		})
	}
}

func TestPayload_RejectsMisfits(t *testing.T) {
	op := Op{Kind: OpEditText}
	if err := op.EncodeAfter(MoveFields{}); err == nil {
		t.Error("move payload accepted on edit_text")
	}
	if err := op.EncodeAfter(nil); err == nil {
		t.Error("null accepted on edit_text")
	}
	op = Op{Kind: OpInsert}
	if err := op.EncodeBefore(InsertFields{}); err == nil {
		t.Error("before payload accepted on insert")
	}
	if err := op.EncodeAfter(nil); err == nil {
		t.Error("null after accepted on insert")
	}
	// A nil pointer is null, not a panic.
	op = Op{Kind: OpEditPage}
	var noPage *PageFields
	if err := op.EncodeAfter(noPage); err != nil || string(op.After) != "null" {
		t.Errorf("nil page: %s, %v", op.After, err)
	}
	op = Op{Kind: OpEditText}
	var noText *TextFields
	if err := op.EncodeAfter(noText); err == nil {
		t.Error("nil text accepted on edit_text")
	}
	op = Op{Kind: "paint"}
	if err := op.EncodeAfter(TextFields{}); err == nil {
		t.Error("unknown kind accepted")
	}
	bad := []Op{
		{Kind: OpInsert, Before: json.RawMessage(`{"text":"x"}`)},
		{Kind: "paint", Before: jsonNull},
		{Kind: OpEditText, Before: jsonNull},
		{Kind: OpEditText, Before: json.RawMessage(`{"text":1}`)},
		{Kind: OpEditText},
		{Kind: OpEditText, Before: json.RawMessage(`{}`)},
		{Kind: OpEditText, Before: json.RawMessage(`{"text":null}`)},
		{Kind: OpSetTask, Before: json.RawMessage(`{}`)},
		{Kind: OpMove, Before: json.RawMessage(`{"parent_id":"p"}`)},
		{Kind: OpSetComplete, Before: json.RawMessage(`{}`)},
		{Kind: OpDelete, Before: json.RawMessage(`{"deleted":false,"deleted_at":null}`)},
	}
	for _, o := range bad {
		if _, err := o.DecodeBefore(); err == nil {
			t.Errorf("decoded %s %s", o.Kind, o.Before)
		}
	}
}

// TestInverse covers AC3: inverting twice returns the op for every kind, except
// insert, which inverts to delete and from there to restore (A-002).
func TestInverse(t *testing.T) {
	for _, tt := range payloadCases {
		t.Run(tt.name, func(t *testing.T) {
			op := encodedOp(t, tt.kind, tt.before, tt.after)
			inv := Inverse(op)
			wantKind := map[OpKind]OpKind{OpInsert: OpDelete, OpDelete: OpRestore, OpRestore: OpDelete}[op.Kind]
			if wantKind == "" {
				wantKind = op.Kind
			}
			if inv.Kind != wantKind || inv.NodeID != op.NodeID {
				t.Fatalf("got %s on %s", inv.Kind, inv.NodeID)
			}
			if _, err := inv.DecodeBefore(); err != nil {
				t.Errorf("inverse before: %v", err)
			}
			if _, err := inv.DecodeAfter(); err != nil {
				t.Errorf("inverse after: %v", err)
			}
			twice := Inverse(inv)
			if op.Kind == OpInsert {
				if twice.Kind != OpRestore {
					t.Errorf("got %s, want restore", twice.Kind)
				}
				return
			}
			if !reflect.DeepEqual(twice, op) {
				t.Errorf("got %s %s %s, want %s %s %s", twice.Kind, twice.Before, twice.After, op.Kind, op.Before, op.After)
			}
			if op.Kind != OpDelete && op.Kind != OpRestore && (string(inv.Before) != tt.afterJ || string(inv.After) != tt.beforeJ) {
				t.Errorf("not swapped: %s %s", inv.Before, inv.After)
			}
		})
	}
}

func TestInverse_InsertIsDeleteOfTheNode(t *testing.T) {
	inv := Inverse(encodedOp(t, OpInsert, nil, InsertFields{ParentID: "p"}))
	a, err := inv.DecodeAfter()
	if err != nil {
		t.Fatal(err)
	}
	if d := a.(*DeleteFields); !d.Deleted || d.DeletedAt != nil || d.DeletedBy != nil {
		t.Errorf("got %+v", d)
	}
	b, err := inv.DecodeBefore()
	if err != nil {
		t.Fatal(err)
	}
	if d := b.(*DeleteFields); d.Deleted {
		t.Errorf("got %+v", d)
	}
}

func TestPayload_NullableFieldsMayBeNull(t *testing.T) {
	ok := []Op{
		{Kind: OpSetComplete, Before: json.RawMessage(`{"completed_at":null}`)},
		{Kind: OpDelete, Before: json.RawMessage(`{"deleted":false,"deleted_at":null,"deleted_by":null}`)},
		{Kind: OpEditText, Before: json.RawMessage(`{"text":"x","later":1}`)},
	}
	for _, o := range ok {
		if _, err := o.DecodeBefore(); err != nil {
			t.Errorf("%s: %v", o.Before, err)
		}
	}
}

func TestChangedFields(t *testing.T) {
	done := mustTime(t, stamp)
	other := mustTime(t, "2026-10-18T00:00:00Z")
	base := Node{ID: "n", ParentID: "p", Position: "a0", Text: "x", CreatedAt: *done}
	with := func(f func(*Node)) *Node {
		n := base
		f(&n)
		return &n
	}
	insert := InsertFields{ParentID: "p", Position: "a0", Text: "x", CreatedAt: *done}
	deleted := DeleteFields{Deleted: true, DeletedAt: done, DeletedBy: csPtr("c")}

	tests := []struct {
		name          string
		kind          OpKind
		before, after any
		cur           Current
		want          []string
	}{
		{"insert unchanged", OpInsert, nil, insert, Current{Node: &base}, nil},
		{"insert with later edits", OpInsert, nil, insert, Current{Node: with(func(n *Node) { n.Text, n.IsTask = "y", true })}, []string{"text", "is_task"}},
		{"insert of a purged node", OpInsert, nil, insert, Current{}, []string{"parent_id", "position", "text", "is_task", "is_heading", "completed_at", "created_at"}},
		{"delete still trashed", OpDelete, DeleteFields{}, deleted, Current{Node: with(func(n *Node) { n.DeletedAt, n.DeletedBy = done, "c" })}, nil},
		{"delete since restored", OpDelete, DeleteFields{}, deleted, Current{Node: &base}, []string{"deleted", "deleted_at", "deleted_by"}},
		{"delete by merge ignores merged_into", OpDelete, DeleteFields{}, DeleteFields{Deleted: true, DeletedAt: done, DeletedBy: csPtr("c"), MergedInto: nodePtr("s")},
			Current{Node: with(func(n *Node) { n.DeletedAt, n.DeletedBy = done, "c" })}, nil},
		{"restore still live", OpRestore, deleted, DeleteFields{}, Current{Node: &base}, nil},
		{"restore since deleted", OpRestore, deleted, DeleteFields{}, Current{Node: with(func(n *Node) { n.DeletedAt, n.DeletedBy = other, "d" })}, []string{"deleted", "deleted_at", "deleted_by"}},
		{"text same", OpEditText, TextFields{"a"}, TextFields{"x"}, Current{Node: &base}, nil},
		{"text changed", OpEditText, TextFields{"a"}, TextFields{"b"}, Current{Node: &base}, []string{"text"}},
		{"text on a purged node", OpEditText, TextFields{"a"}, TextFields{"x"}, Current{}, []string{"text"}},
		{"move position only", OpMove, MoveFields{"q", "a1"}, MoveFields{"p", "a9"}, Current{Node: &base}, []string{"position"}},
		{"task", OpSetTask, TaskFields{true}, TaskFields{true}, Current{Node: &base}, []string{"is_task"}},
		{"heading", OpSetHeading, HeadingFields{true}, HeadingFields{false}, Current{Node: &base}, nil},
		{"complete same time", OpSetComplete, CompleteFields{}, CompleteFields{done}, Current{Node: with(func(n *Node) { n.CompletedAt = mustTime(t, stamp) })}, nil},
		{"complete other time", OpSetComplete, CompleteFields{}, CompleteFields{done}, Current{Node: with(func(n *Node) { n.CompletedAt = other })}, []string{"completed_at"}},
		{"complete since cleared", OpSetComplete, CompleteFields{}, CompleteFields{done}, Current{Node: &base}, []string{"completed_at"}},
		{"star same", OpSetStar, StarFields{}, StarFields{done}, Current{StarredAt: done}, nil},
		{"star removed", OpSetStar, StarFields{}, StarFields{done}, Current{}, []string{"starred_at"}},
		{"unstar still off", OpSetStar, StarFields{done}, StarFields{}, Current{}, nil},
		{"page same", OpEditPage, nil, PageFields{"m"}, Current{Page: &Page{Markdown: "m"}}, nil},
		{"page edited", OpEditPage, nil, PageFields{"m"}, Current{Page: &Page{Markdown: "n"}}, []string{"markdown"}},
		{"page since deleted", OpEditPage, nil, PageFields{"m"}, Current{}, []string{"markdown"}},
		{"page delete still gone", OpEditPage, PageFields{"m"}, nil, Current{}, nil},
		{"page since recreated", OpEditPage, PageFields{"m"}, nil, Current{Page: &Page{Markdown: "n"}}, []string{"markdown"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ChangedFields(encodedOp(t, tt.kind, tt.before, tt.after), tt.cur)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	if _, err := ChangedFields(Op{Kind: OpEditText, After: json.RawMessage(`[`)}, Current{}); err == nil {
		t.Error("malformed payload accepted")
	}
}
