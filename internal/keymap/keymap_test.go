package keymap

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestDefaults_Valid(t *testing.T) {
	for _, tb := range []Table{Defaults(), Defaults().Resolve(Linux), Defaults().Resolve(MacOS)} {
		if ps := tb.Validate(); len(ps) > 0 {
			t.Errorf("platform %q: %d problems:\n%v", tb.Platform(), len(ps), ps)
		}
	}
}

// withBinding returns the defaults plus extra bindings or commands.
func with(edit func(*Table)) Table {
	t := Defaults()
	edit(&t)
	return t
}

func TestValidate_Rules(t *testing.T) {
	tests := []struct {
		name     string
		table    Table
		rule     string
		platform string
	}{
		{"key bound twice in one context", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "enter", Command: Indent})
		}), RuleDuplicate, ""},
		{"mod collides on one platform", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "ctrl+home", Command: LastBullet})
		}), RuleDuplicate, Linux},
		{"block command without a single key", with(func(t *Table) {
			t.bindings = slices.DeleteFunc(t.bindings, func(b Binding) bool { return b.Command == Duplicate })
			t.bindings = append(t.bindings, Binding{Context: Block, Key: "mod+d", Command: Duplicate})
		}), RuleBlockKey, ""},
		{"reserved key", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Global, Key: "mod+w", Command: Settings})
		}), RuleReserved, ""},
		{"reserved ctrl+tab", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "ctrl+tab", Command: Indent})
		}), RuleReserved, ""},
		{"reserved on macOS only", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "meta+q", Command: Indent, Platform: MacOS})
		}), RuleReserved, MacOS},
		{"malformed key", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "shift+mod+x", Command: Indent})
		}), RuleSyntax, ""},
		{"unknown command", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "f9", Command: "no_such"})
		}), RuleUnknown, ""},
		{"duplicate command", with(func(t *Table) {
			t.commands = append(t.commands, Command{ID: Undo, Name: "Undo", Contexts: []Context{Global}})
		}), RuleDupCommand, ""},
		{"wrong context", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Trash, Key: "f9", Command: NewBullet})
		}), RuleContext, ""},
		{"palette-only command with a key", with(func(t *Table) {
			t.bindings = append(t.bindings, Binding{Context: Text, Key: "f9", Command: CollapseAll})
		}), RulePalette, ""},
		{"command without a key", with(func(t *Table) {
			t.bindings = slices.DeleteFunc(t.bindings, func(b Binding) bool { return b.Command == PageFind })
		}), RuleUnbound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := tt.table.Validate()
			if len(ps) != 1 {
				t.Fatalf("want one problem, got %d: %v", len(ps), ps)
			}
			if ps[0].Rule != tt.rule || ps[0].Platform != tt.platform {
				t.Errorf("got rule %s on %q (%v), want %s on %q", ps[0].Rule, ps[0].Platform, ps[0], tt.rule, tt.platform)
			}
		})
	}
}

// specBlockTable is §6.4's block-selection table, row by row, in key
// notation. "Other printable keys" has no binding: the page shows a hint.
var specBlockTable = []struct{ keys, command string }{
	{"arrowup / arrowdown", "selection_up / selection_down"}, // Up / Down: move the selection
	{"shift+arrowup / shift+arrowdown", "selection_extend_up / selection_extend_down"},
	{"- / =", "collapse / expand"},
	{"1 2 3 4 5 6 7 8 9", "expand_level_1 expand_level_2 expand_level_3 expand_level_4 expand_level_5 expand_level_6 expand_level_7 expand_level_8 expand_level_9"},
	{"0", "expand_all"},
	{"tab / shift+tab", "indent / outdent"},
	{"[ / ]", "move_up / move_down"},
	{"mod+shift+arrowup / mod+shift+arrowdown", "move_up / move_down"}, // "also Ctrl+Shift+Up / Down"
	{"z / Z", "zoom_in / zoom_out"},
	{"~", "zoom_root"},
	{"enter", "edit_text"},
	{"x", "toggle_complete"},
	{"t", "toggle_task"},
	{"#", "toggle_heading"},
	{"delete d", "delete_subtree delete_subtree"},
	{"backspace", "delete_hint"},
	{"mod+c", "copy"},
	{"mod+x", "cut"},
	{"p", "open_page"},
	{"o", "follow_link"},
	{"D", "duplicate"},
	{"h", "toggle_show_completed"},
	{"c", "color_picker"},
	{"s", "toggle_star"},
	{"m", "move_to"},
	{"/", "filter"},
	{"?", "help"},
	{"escape", "clear_filter"},
}

// textChordsInBlock are the text-editing chords that act on bullets, which
// also work in block selection (§6.4 "Block selection").
var textChordsInBlock = map[string]string{"mod+enter": "toggle_complete", "mod+f": "filter"}

func TestDefaults_BlockTableMatchesSpec(t *testing.T) {
	want := map[string]string{}
	for _, row := range specBlockTable {
		keys := strings.Fields(strings.ReplaceAll(row.keys, " / ", " "))
		cmds := strings.Fields(strings.ReplaceAll(row.command, " / ", " "))
		if len(keys) != len(cmds) {
			t.Fatalf("row %q: %d keys, %d commands", row.keys, len(keys), len(cmds))
		}
		for i, k := range keys {
			want[k] = cmds[i]
		}
	}
	for k, c := range textChordsInBlock {
		want[k] = c
	}
	got := map[string]string{}
	for _, b := range Defaults().Bindings() {
		if b.Context == Block {
			if b.Platform != "" {
				t.Errorf("block binding %s is for %s only", b.Key, b.Platform)
			}
			got[b.Key] = string(b.Command)
		}
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("block %s: got %q, want %q", k, got[k], c)
		}
	}
	for k, c := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("block %s → %s is not in §6.4's table", k, c)
		}
	}
	// Every single key of the table is a block-selection command.
	tb := Defaults()
	for k, c := range want {
		cmd, _ := tb.Command(CommandID(c))
		if singleKey(k) && !cmd.BlockSelectionCommand {
			t.Errorf("%s (%s) is not marked a block-selection command", c, k)
		}
	}
}

func TestParseKey(t *testing.T) {
	ok := []string{"mod+shift+arrowup", "escape", "#", "?", "Z", "mod+,", "ctrl+a", "meta+[", "alt+shift+arrowleft", "shift+tab", "f1", "mod+shift+z"}
	for _, k := range ok {
		if _, _, err := ParseKey(k); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	bad := []string{"", "mod+", "shift+mod+x", "ctrl+ctrl+x", "mod+ctrl+x", "shift+z", "mod+Z", "ArrowUp", "hyper+x", "spacebar", "ctrl+shift"}
	for _, k := range bad {
		if _, _, err := ParseKey(k); err == nil {
			t.Errorf("%s: no error", k)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct{ key, linux, mac string }{
		{"mod+shift+arrowup", "ctrl+shift+arrowup", "meta+shift+arrowup"},
		{"mod+alt+x", "ctrl+alt+x", "meta+alt+x"},
		{"ctrl+tab", "ctrl+tab", "ctrl+tab"},
		{"?", "?", "?"},
	}
	for _, tt := range tests {
		if got := resolveKey(tt.key, Linux); got != tt.linux {
			t.Errorf("%s on Linux: %s, want %s", tt.key, got, tt.linux)
		}
		if got := resolveKey(tt.key, MacOS); got != tt.mac {
			t.Errorf("%s on macOS: %s, want %s", tt.key, got, tt.mac)
		}
	}
	if Platform("darwin") != MacOS || Platform("linux") != Linux || Platform("freebsd") != Linux {
		t.Error("Platform")
	}
}

func TestLookup_ContextThenGlobal(t *testing.T) {
	lin, mac := Defaults().Resolve(Linux), Defaults().Resolve(MacOS)
	tests := []struct {
		tb   Table
		ctx  Context
		key  string
		want CommandID
	}{
		{lin, Text, "ctrl+z", Undo}, // global
		{mac, Text, "meta+z", Undo},
		{lin, Text, "alt+arrowleft", ZoomBack}, // global on Linux
		{mac, Text, "alt+arrowleft", MoveWord}, // text wins on macOS
		{lin, Text, "ctrl+a", SelectAllText},   // D-043
		{mac, Text, "ctrl+a", LineStart},       // macOS keeps Ctrl+A
		{mac, Text, "meta+a", SelectAllText},
		{lin, Block, "arrowup", SelectionUp},
		{lin, Block, "?", Help},
		{lin, Text, "f1", Help},
		{lin, Trash, "d", TrashPurge},
	}
	for _, tt := range tests {
		got, ok := tt.tb.Lookup(tt.ctx, tt.key)
		if !ok || got != tt.want {
			t.Errorf("%s %s %s: %q, want %q", tt.tb.Platform(), tt.ctx, tt.key, got, tt.want)
		}
	}
	for _, k := range []string{"ctrl+w", "meta+w", "x"} {
		if c, ok := lin.Lookup(Text, k); ok {
			t.Errorf("text %s is bound to %s", k, c)
		}
	}
}

func TestJSON_EveryCommandWithItsKeys(t *testing.T) {
	tb := Defaults().Resolve(MacOS)
	var got []struct {
		ID       string    `json:"id"`
		Name     string    `json:"name"`
		Contexts []Context `json:"contexts"`
		Native   bool      `json:"native"`
		Keys     []struct {
			Context Context `json:"context"`
			Key     string  `json:"key"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(tb.JSON(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(tb.Commands()) {
		t.Fatalf("%d commands, want %d", len(got), len(tb.Commands()))
	}
	n := 0
	for _, c := range got {
		if c.Name == "" || len(c.Contexts) == 0 {
			t.Errorf("%s: no name or contexts", c.ID)
		}
		for _, k := range c.Keys {
			n++
			if strings.Contains(k.Key, "mod") {
				t.Errorf("%s: unresolved key %s", c.ID, k.Key)
			}
		}
	}
	if n != len(tb.Bindings()) {
		t.Errorf("%d keys, want %d", n, len(tb.Bindings()))
	}
}

// TestDefaults_CommandIDsAreSnakeCase keeps the IDs usable as action names
// (CONTRACTS §12.1).
func TestDefaults_CommandIDsAreSnakeCase(t *testing.T) {
	var ids []string
	for _, c := range Defaults().Commands() {
		for _, r := range c.ID {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
				t.Errorf("%s is not snake_case", c.ID)
				break
			}
		}
		ids = append(ids, string(c.ID))
	}
	sort.Strings(ids)
	t.Logf("%d commands", len(ids))
}
