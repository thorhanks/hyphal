package keymap

import "fmt"

// CommandID names a command: in the keymap, the palette, help and the UI
// protocol's action names (CONTRACTS §12.1).
type CommandID string

// Commands of §6.4 and §6.6.
const (
	// Global.
	Help           CommandID = "help"
	CommandPalette CommandID = "command_palette"
	Settings       CommandID = "settings"
	JumpPalette    CommandID = "jump_palette"
	Undo           CommandID = "undo"
	Redo           CommandID = "redo"
	SaveHint       CommandID = "save_hint"
	ZoomBack       CommandID = "zoom_back"
	ZoomForward    CommandID = "zoom_forward"

	// Text editing.
	NewBullet          CommandID = "new_bullet"
	Backspace          CommandID = "backspace"
	DeleteForward      CommandID = "delete_forward"
	Indent             CommandID = "indent"
	Outdent            CommandID = "outdent"
	BlockSelect        CommandID = "block_select"
	CursorUp           CommandID = "cursor_up"
	CursorDown         CommandID = "cursor_down"
	CursorLeft         CommandID = "cursor_left"
	CursorRight        CommandID = "cursor_right"
	MoveWord           CommandID = "move_word"
	LineStart          CommandID = "line_start"
	LineEnd            CommandID = "line_end"
	FirstBullet        CommandID = "first_bullet"
	LastBullet         CommandID = "last_bullet"
	PageScroll         CommandID = "page_scroll"
	DeleteWordBackward CommandID = "delete_word_backward"
	SelectAllText      CommandID = "select_all_text"
	ExtendSelection    CommandID = "extend_selection"
	WrapSelection      CommandID = "wrap_selection"
	CopyText           CommandID = "copy_text"
	CutText            CommandID = "cut_text"
	Paste              CommandID = "paste"
	MoveUp             CommandID = "move_up"
	MoveDown           CommandID = "move_down"
	ToggleComplete     CommandID = "toggle_complete"
	OpenFilter         CommandID = "filter"

	// Block selection.
	SelectionUp         CommandID = "selection_up"
	SelectionDown       CommandID = "selection_down"
	SelectionExtendUp   CommandID = "selection_extend_up"
	SelectionExtendDown CommandID = "selection_extend_down"
	Collapse            CommandID = "collapse"
	Expand              CommandID = "expand"
	ExpandAll           CommandID = "expand_all"
	ZoomIn              CommandID = "zoom_in"
	ZoomOut             CommandID = "zoom_out"
	ZoomRoot            CommandID = "zoom_root"
	EditText            CommandID = "edit_text"
	ToggleTask          CommandID = "toggle_task"
	ToggleHeading       CommandID = "toggle_heading"
	DeleteSubtree       CommandID = "delete_subtree"
	DeleteHint          CommandID = "delete_hint"
	Copy                CommandID = "copy"
	Cut                 CommandID = "cut"
	OpenPage            CommandID = "open_page"
	FollowLink          CommandID = "follow_link"
	Duplicate           CommandID = "duplicate"
	ToggleShowCompleted CommandID = "toggle_show_completed"
	ColorPicker         CommandID = "color_picker"
	ToggleStar          CommandID = "toggle_star"
	MoveTo              CommandID = "move_to"
	ClearFilter         CommandID = "clear_filter"

	// Filter input.
	FilterAccept CommandID = "filter_accept"

	// Page.
	PageNewline CommandID = "page_newline"
	PageIndent  CommandID = "page_indent"
	PageFind    CommandID = "page_find"
	ClosePage   CommandID = "close_page"

	// Overlays.
	OverlayUp     CommandID = "overlay_up"
	OverlayDown   CommandID = "overlay_down"
	OverlayChoose CommandID = "overlay_choose"
	OverlayClose  CommandID = "overlay_close"

	// Trash view.
	TrashUp      CommandID = "trash_up"
	TrashDown    CommandID = "trash_down"
	TrashPreview CommandID = "trash_preview"
	TrashRestore CommandID = "trash_restore"
	TrashPurge   CommandID = "trash_purge"
	TrashClose   CommandID = "trash_close"

	// Palette only (§6.6, P1-38).
	CollapseAll   CommandID = "collapse_all"
	DeletePage    CommandID = "delete_page"
	BoldBullets   CommandID = "bold_bullets"
	ItalicBullets CommandID = "italic_bullets"
	ColorBullets  CommandID = "color_bullets"
	OpenTrash     CommandID = "open_trash"
	EmptyTrash    CommandID = "empty_trash"
	SortChildren  CommandID = "sort_children"
	CopyLink      CommandID = "copy_link"
)

// ExpandLevel returns the command for "Expand to level n", n in 1…9.
func ExpandLevel(n int) CommandID { return CommandID(fmt.Sprintf("expand_level_%d", n)) }

// Defaults returns the default keymap of §6.4, unresolved: "mod" stands for
// Ctrl on Linux and Cmd on macOS. Each call builds a new table.
func Defaults() Table {
	var t Table
	cmd := func(id CommandID, name string, phase int, ctxs ...Context) *Command {
		t.commands = append(t.commands, Command{ID: id, Name: name, Phase: phase, Contexts: ctxs})
		return &t.commands[len(t.commands)-1]
	}
	bind := func(ctx Context, id CommandID, keys ...string) {
		for _, k := range keys {
			t.bindings = append(t.bindings, Binding{Context: ctx, Key: k, Command: id})
		}
	}
	// on binds keys for one platform only.
	on := func(platform string, ctx Context, id CommandID, keys ...string) {
		for _, k := range keys {
			t.bindings = append(t.bindings, Binding{Context: ctx, Key: k, Command: id, Platform: platform})
		}
	}
	native := func(c *Command) { c.Native = true; c.Hidden = true }
	block := func(c *Command) *Command { c.BlockSelectionCommand = true; return c }
	outline := []Context{Text, Block}

	// Global (§6.4).
	block(cmd(Help, "Help", 1, Global, Block))
	cmd(CommandPalette, "Command palette", 1, Global)
	cmd(Settings, "Settings", 1, Global)
	cmd(JumpPalette, "Jump to…", 2, Global)
	cmd(Undo, "Undo", 1, Global)
	cmd(Redo, "Redo", 1, Global)
	cmd(SaveHint, "Save", 1, Global)
	cmd(ZoomBack, "Zoom back", 1, Global).Native = true
	cmd(ZoomForward, "Zoom forward", 1, Global).Native = true
	bind(Global, Help, "f1")
	bind(Global, CommandPalette, "mod+p")
	bind(Global, Settings, "mod+,")
	bind(Global, JumpPalette, "mod+k")
	bind(Global, Undo, "mod+z")
	bind(Global, Redo, "mod+y", "mod+shift+z")
	bind(Global, SaveHint, "mod+s")
	on(Linux, Global, ZoomBack, "alt+arrowleft")
	on(Linux, Global, ZoomForward, "alt+arrowright")
	on(MacOS, Global, ZoomBack, "meta+[")
	on(MacOS, Global, ZoomForward, "meta+]")

	// Text editing (§6.4).
	cmd(NewBullet, "New bullet", 1, Text)
	cmd(Backspace, "Backspace", 1, Text).Hidden = true
	cmd(DeleteForward, "Delete", 1, Text).Hidden = true
	block(cmd(Indent, "Indent", 1, outline...))
	block(cmd(Outdent, "Outdent", 1, outline...))
	cmd(BlockSelect, "Select bullet", 1, Text)
	cmd(CursorUp, "Up", 0, Text).Hidden = true
	cmd(CursorDown, "Down", 0, Text).Hidden = true
	cmd(CursorLeft, "Left", 0, Text).Hidden = true
	cmd(CursorRight, "Right", 0, Text).Hidden = true
	native(cmd(MoveWord, "Move by word", 1, Text))
	cmd(LineStart, "Start of line", 1, Text).Hidden = true
	cmd(LineEnd, "End of line", 1, Text).Hidden = true
	cmd(FirstBullet, "First bullet", 1, Text)
	cmd(LastBullet, "Last bullet", 1, Text)
	native(cmd(PageScroll, "Scroll a screen", 1, Text))
	native(cmd(DeleteWordBackward, "Delete word backward", 1, Text))
	native(cmd(SelectAllText, "Select all text", 1, Text))
	native(cmd(ExtendSelection, "Extend selection", 1, Text))
	cmd(WrapSelection, "Wrap selection", 1, Text).Hidden = true
	native(cmd(CopyText, "Copy text", 1, Text))
	native(cmd(CutText, "Cut text", 1, Text))
	native(cmd(Paste, "Paste", 1, Text))
	block(cmd(MoveUp, "Move up", 1, outline...))
	block(cmd(MoveDown, "Move down", 1, outline...))
	block(cmd(ToggleComplete, "Toggle complete", 1, outline...))
	block(cmd(OpenFilter, "Filter", 1, outline...))

	bind(Text, NewBullet, "enter")
	bind(Text, Backspace, "backspace")
	bind(Text, DeleteForward, "delete")
	bind(Text, Indent, "tab")
	bind(Text, Outdent, "shift+tab")
	bind(Text, BlockSelect, "escape")
	bind(Text, CursorUp, "arrowup")
	bind(Text, CursorDown, "arrowdown")
	bind(Text, CursorLeft, "arrowleft")
	bind(Text, CursorRight, "arrowright")
	on(Linux, Text, MoveWord, "ctrl+arrowleft", "ctrl+arrowright")
	on(MacOS, Text, MoveWord, "alt+arrowleft", "alt+arrowright")
	bind(Text, LineStart, "home")
	bind(Text, LineEnd, "end")
	on(MacOS, Text, LineStart, "meta+arrowleft", "ctrl+a")
	on(MacOS, Text, LineEnd, "meta+arrowright", "ctrl+e")
	bind(Text, FirstBullet, "mod+home")
	bind(Text, LastBullet, "mod+end")
	on(MacOS, Text, FirstBullet, "meta+arrowup")
	on(MacOS, Text, LastBullet, "meta+arrowdown")
	bind(Text, PageScroll, "pageup", "pagedown")
	on(Linux, Text, DeleteWordBackward, "ctrl+backspace")
	on(MacOS, Text, DeleteWordBackward, "alt+backspace")
	bind(Text, SelectAllText, "mod+a")
	bind(Text, ExtendSelection, "shift+arrowleft", "shift+arrowright", "shift+arrowup", "shift+arrowdown",
		"shift+home", "shift+end", "mod+shift+arrowleft", "mod+shift+arrowright")
	on(MacOS, Text, ExtendSelection, "alt+shift+arrowleft", "alt+shift+arrowright")
	bind(Text, WrapSelection, "*", "~", "=", "`")
	bind(Text, CopyText, "mod+c")
	bind(Text, CutText, "mod+x")
	bind(Text, Paste, "mod+v")
	bind(Text, MoveUp, "mod+shift+arrowup")
	bind(Text, MoveDown, "mod+shift+arrowdown")
	bind(Text, ToggleComplete, "mod+enter")
	bind(Text, OpenFilter, "mod+f")

	// Block selection (§6.4). Chords from text editing that act on bullets
	// work here too.
	for _, c := range []struct {
		id   CommandID
		name string
	}{
		{SelectionUp, "Selection up"}, {SelectionDown, "Selection down"},
		{SelectionExtendUp, "Extend selection up"}, {SelectionExtendDown, "Extend selection down"},
	} {
		block(cmd(c.id, c.name, 1, Block)).Hidden = true
	}
	block(cmd(Collapse, "Collapse", 1, outline...))
	block(cmd(Expand, "Expand", 1, outline...))
	for n := 1; n <= 9; n++ {
		block(cmd(ExpandLevel(n), fmt.Sprintf("Expand to level %d", n), 1, outline...))
	}
	block(cmd(ExpandAll, "Expand all", 1, outline...))
	block(cmd(ZoomIn, "Zoom in", 1, outline...))
	block(cmd(ZoomOut, "Zoom out", 1, outline...))
	block(cmd(ZoomRoot, "Zoom to Home", 1, outline...))
	block(cmd(EditText, "Edit text", 1, Block))
	block(cmd(ToggleTask, "Toggle task", 1, outline...))
	block(cmd(ToggleHeading, "Toggle heading", 1, outline...))
	block(cmd(DeleteSubtree, "Delete", 1, outline...))
	block(cmd(DeleteHint, "Delete hint", 1, Block)).Hidden = true
	cmd(Copy, "Copy as Markdown", 1, Block)
	cmd(Cut, "Cut", 1, Block)
	block(cmd(OpenPage, "Open page", 1, outline...))
	block(cmd(FollowLink, "Follow link", 1, Text, Block, Page))
	block(cmd(Duplicate, "Duplicate", 1, outline...))
	block(cmd(ToggleShowCompleted, "Show or hide completed", 1, outline...))
	block(cmd(ColorPicker, "Color picker", 1, outline...))
	block(cmd(ToggleStar, "Toggle star", 2, outline...))
	block(cmd(MoveTo, "Move to…", 1, outline...))
	block(cmd(ClearFilter, "Clear filter", 1, Block, Filter))

	bind(Block, SelectionUp, "arrowup")
	bind(Block, SelectionDown, "arrowdown")
	bind(Block, SelectionExtendUp, "shift+arrowup")
	bind(Block, SelectionExtendDown, "shift+arrowdown")
	bind(Block, Collapse, "-")
	bind(Block, Expand, "=")
	for n := 1; n <= 9; n++ {
		bind(Block, ExpandLevel(n), fmt.Sprint(n))
	}
	bind(Block, ExpandAll, "0")
	bind(Block, Indent, "tab")
	bind(Block, Outdent, "shift+tab")
	bind(Block, MoveUp, "[", "mod+shift+arrowup")
	bind(Block, MoveDown, "]", "mod+shift+arrowdown")
	bind(Block, ZoomIn, "z")
	bind(Block, ZoomOut, "Z")
	bind(Block, ZoomRoot, "~")
	bind(Block, EditText, "enter")
	bind(Block, ToggleComplete, "x", "mod+enter")
	bind(Block, ToggleTask, "t")
	bind(Block, ToggleHeading, "#")
	bind(Block, DeleteSubtree, "delete", "d")
	bind(Block, DeleteHint, "backspace")
	bind(Block, Copy, "mod+c")
	bind(Block, Cut, "mod+x")
	bind(Block, OpenPage, "p")
	bind(Block, FollowLink, "o")
	bind(Block, Duplicate, "D")
	bind(Block, ToggleShowCompleted, "h")
	bind(Block, ColorPicker, "c")
	bind(Block, ToggleStar, "s")
	bind(Block, MoveTo, "m")
	bind(Block, OpenFilter, "/", "mod+f")
	bind(Block, Help, "?")
	bind(Block, ClearFilter, "escape")

	// Filter input (§6.2, §6.4).
	cmd(FilterAccept, "Go to the outline", 1, Filter).Hidden = true
	bind(Filter, FilterAccept, "enter", "arrowdown")
	bind(Filter, ClearFilter, "escape")

	// Page (§5.7, §6.4).
	cmd(PageNewline, "New line", 1, Page).Hidden = true
	cmd(PageIndent, "Insert two spaces", 1, Page).Hidden = true
	cmd(PageFind, "Find in page", 1, Page)
	cmd(ClosePage, "Close page", 1, Page)
	bind(Page, PageNewline, "enter")
	bind(Page, PageIndent, "tab")
	bind(Page, FollowLink, "mod+enter")
	bind(Page, PageFind, "mod+f")
	bind(Page, ClosePage, "escape")

	// Overlays (§6.4). The color picker's digits are its own list keys, like
	// typing in a palette (A-014).
	cmd(OverlayUp, "Up", 1, Overlay).Hidden = true
	cmd(OverlayDown, "Down", 1, Overlay).Hidden = true
	cmd(OverlayChoose, "Choose", 1, Overlay).Hidden = true
	cmd(OverlayClose, "Close", 1, Overlay).Hidden = true
	bind(Overlay, OverlayUp, "arrowup")
	bind(Overlay, OverlayDown, "arrowdown")
	bind(Overlay, OverlayChoose, "enter")
	bind(Overlay, OverlayClose, "escape")

	// Trash view (§4.8, §6.4).
	cmd(TrashUp, "Up", 1, Trash).Hidden = true
	cmd(TrashDown, "Down", 1, Trash).Hidden = true
	cmd(TrashPreview, "Preview deletion", 1, Trash)
	cmd(TrashRestore, "Restore", 1, Trash)
	cmd(TrashPurge, "Purge", 1, Trash)
	cmd(TrashClose, "Back to the outline", 1, Trash)
	bind(Trash, TrashUp, "arrowup")
	bind(Trash, TrashDown, "arrowdown")
	bind(Trash, TrashPreview, "enter")
	bind(Trash, TrashRestore, "r")
	bind(Trash, TrashPurge, "d")
	bind(Trash, TrashClose, "escape")

	// Palette only (§6.6, P1-38).
	for _, c := range []struct {
		id    CommandID
		name  string
		phase int
		ctxs  []Context
	}{
		{CollapseAll, "Collapse all", 1, outline},
		{DeletePage, "Delete page", 1, outline},
		{BoldBullets, "Bold", 1, outline},
		{ItalicBullets, "Italic", 1, outline},
		{ColorBullets, "Color…", 1, outline},
		{OpenTrash, "Open Trash", 1, outline},
		{EmptyTrash, "Empty Trash", 1, []Context{Text, Block, Trash}},
		{SortChildren, "Sort children", 2, outline},
		{CopyLink, "Copy link to node", 2, outline},
	} {
		cmd(c.id, c.name, c.phase, c.ctxs...).PaletteOnly = true
	}
	return t
}
