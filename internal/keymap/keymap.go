package keymap

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Context is where a binding applies (§6.4 "Contexts").
type Context string

// The contexts of §6.4. Lookup tries the current context, then Global.
const (
	Global  Context = "global"
	Text    Context = "text"
	Block   Context = "block"
	Filter  Context = "filter"
	Page    Context = "page"
	Overlay Context = "overlay"
	Trash   Context = "trash"
)

// Contexts lists every context in display order.
var Contexts = []Context{Global, Text, Block, Filter, Page, Overlay, Trash}

// Platforms a table resolves for. Mod is Ctrl on Linux and Meta on macOS
// (§6.4).
const (
	Linux = "linux"
	MacOS = "darwin"
)

// Platform maps a GOOS to the keymap platform: macOS, or Linux for anything
// else.
func Platform(goos string) string {
	if goos == MacOS {
		return MacOS
	}
	return Linux
}

// Command is one command's metadata, used by the palette, help and key
// dispatch (§6.4, §6.6).
type Command struct {
	ID       CommandID
	Name     string    // palette and help name
	Phase    int       // the phase it arrives in
	Contexts []Context // where it can run
	// PaletteOnly commands have no default key by design (§6.6).
	PaletteOnly bool
	// BlockSelectionCommand commands need a single key in block selection
	// (§6.4 "Two layers").
	BlockSelectionCommand bool
	// Native commands are performed by the browser: the page never cancels
	// their keys (§6.4 "Browser rules").
	Native bool
	// Hidden commands are movement inside a context; the palette leaves them
	// out.
	Hidden bool
}

// Binding binds one key to a command in one context. Platform is empty for
// both platforms, or Linux or MacOS for one of them.
type Binding struct {
	Context  Context
	Key      string
	Command  CommandID
	Platform string
	orig     string // the unresolved key, after Resolve
}

// Table is a keymap: the commands and their bindings. A table from Defaults
// writes keys with "mod" for both platforms; Resolve picks one platform.
type Table struct {
	commands []Command
	bindings []Binding
	platform string // "" until resolved
}

// Commands returns the commands in table order.
func (t Table) Commands() []Command { return slices.Clone(t.commands) }

// Bindings returns the bindings in table order.
func (t Table) Bindings() []Binding { return slices.Clone(t.bindings) }

// Platform returns the platform the table is resolved for, or "".
func (t Table) Platform() string { return t.platform }

// Command returns the metadata of id.
func (t Table) Command(id CommandID) (Command, bool) {
	i := slices.IndexFunc(t.commands, func(c Command) bool { return c.ID == id })
	if i < 0 {
		return Command{}, false
	}
	return t.commands[i], true
}

// Resolve returns the table for one platform: bindings for the other
// platform are dropped and "mod" becomes ctrl or meta, in canonical order.
func (t Table) Resolve(platform string) Table {
	out := Table{commands: t.commands, platform: platform}
	for _, b := range t.bindings {
		if b.Platform != "" && b.Platform != platform {
			continue
		}
		b.orig, b.Key = b.Key, resolveKey(b.Key, platform)
		b.Platform = ""
		out.bindings = append(out.bindings, b)
	}
	return out
}

// Lookup finds the command bound to key in ctx, then in Global. The table
// must be resolved.
func (t Table) Lookup(ctx Context, key string) (CommandID, bool) {
	for _, c := range []Context{ctx, Global} {
		for _, b := range t.bindings {
			if b.Context == c && b.Key == key {
				return b.Command, true
			}
		}
	}
	return "", false
}

// jsonKey and jsonCommand are the keymap's wire form (CONTRACTS §12.2).
type jsonKey struct {
	Context Context `json:"context"`
	Key     string  `json:"key"`
}

type jsonCommand struct {
	ID          CommandID `json:"id"`
	Name        string    `json:"name"`
	Phase       int       `json:"phase"`
	Contexts    []Context `json:"contexts"`
	PaletteOnly bool      `json:"paletteOnly,omitempty"`
	Block       bool      `json:"block,omitempty"`
	Native      bool      `json:"native,omitempty"`
	Hidden      bool      `json:"hidden,omitempty"`
	Keys        []jsonKey `json:"keys"`
}

// JSON returns the keymap as sent in welcome (CONTRACTS §12.2): one entry
// per command, with its keys. Resolve the table first.
func (t Table) JSON() []byte {
	out := make([]jsonCommand, 0, len(t.commands))
	for _, c := range t.commands {
		jc := jsonCommand{
			ID: c.ID, Name: c.Name, Phase: c.Phase, Contexts: c.Contexts,
			PaletteOnly: c.PaletteOnly, Block: c.BlockSelectionCommand, Native: c.Native, Hidden: c.Hidden,
			Keys: []jsonKey{},
		}
		for _, b := range t.bindings {
			if b.Command == c.ID {
				jc.Keys = append(jc.Keys, jsonKey{b.Context, b.Key})
			}
		}
		out = append(out, jc)
	}
	b, err := json.Marshal(out)
	if err != nil {
		panic(err) // only strings, ints and bools
	}
	return b
}

// Key notation (§6.4): modifiers then the key, joined by "+". Modifiers come
// in the order mod, ctrl, meta, alt, shift. The key is a named key in
// lowercase event.key form ("arrowup", "escape", "f1", "space"), or one
// character as event.key gives it. Without ctrl, meta, mod or alt a
// character carries its own shift ("Z", "?", "#") and "shift" is not
// written; with them, letters are lowercase and "shift" is written
// ("mod+shift+z"). The page builds the same strings (lib/keys.js).
var modOrder = []string{"mod", "ctrl", "meta", "alt", "shift"}

var namedKeys = map[string]bool{
	"escape": true, "enter": true, "tab": true, "backspace": true, "delete": true, "insert": true,
	"arrowup": true, "arrowdown": true, "arrowleft": true, "arrowright": true,
	"home": true, "end": true, "pageup": true, "pagedown": true, "space": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true, "f6": true,
	"f7": true, "f8": true, "f9": true, "f10": true, "f11": true, "f12": true,
}

// ParseKey splits a key string into its modifiers and key, and checks that
// it is canonical.
func ParseKey(s string) (mods []string, key string, err error) {
	parts := strings.Split(s, "+")
	key = parts[len(parts)-1]
	mods = parts[:len(parts)-1]
	if key == "" {
		return nil, "", fmt.Errorf("key %q: no key after the modifiers", s)
	}
	last := -1
	for _, m := range mods {
		i := slices.Index(modOrder, m)
		if i < 0 {
			return nil, "", fmt.Errorf("key %q: unknown modifier %q", s, m)
		}
		if i <= last {
			return nil, "", fmt.Errorf("key %q: modifiers repeat or are out of order (%s)", s, strings.Join(modOrder, ", "))
		}
		last = i
	}
	if slices.Contains(mods, "mod") && (slices.Contains(mods, "ctrl") || slices.Contains(mods, "meta")) {
		return nil, "", fmt.Errorf("key %q: mod already means ctrl or meta", s)
	}
	chord := slices.ContainsFunc(mods, func(m string) bool { return m != "shift" })
	switch {
	case namedKeys[key]:
	case utf8.RuneCountInString(key) == 1:
		if !chord && slices.Contains(mods, "shift") {
			return nil, "", fmt.Errorf("key %q: write the shifted character instead of shift", s)
		}
		if chord && strings.ToLower(key) != key {
			return nil, "", fmt.Errorf("key %q: write shift and a lowercase letter in a chord", s)
		}
	default:
		return nil, "", fmt.Errorf("key %q: unknown key %q", s, key)
	}
	return mods, key, nil
}

// Resolve returns one key string for a platform: mod becomes ctrl or meta,
// in canonical order. A malformed key is returned unchanged.
func Resolve(key, platform string) string { return resolveKey(key, platform) }

// resolveKey replaces mod with ctrl or meta and keeps canonical order.
func resolveKey(s, platform string) string {
	mods, key, err := ParseKey(s)
	if err != nil {
		return s // Validate reports it
	}
	mod := "ctrl"
	if platform == MacOS {
		mod = "meta"
	}
	set := map[string]bool{}
	for _, m := range mods {
		if m == "mod" {
			m = mod
		}
		set[m] = true
	}
	var out []string
	for _, m := range modOrder {
		if set[m] {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, key), "+")
}

// singleKey reports whether a key needs no ctrl, meta or alt: shift is fine
// (§6.4 "Two layers").
func singleKey(s string) bool {
	mods, _, err := ParseKey(s)
	return err == nil && !slices.ContainsFunc(mods, func(m string) bool { return m != "shift" })
}
