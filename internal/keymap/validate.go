package keymap

import (
	"fmt"
	"slices"
)

// Problem is one broken rule of a keymap (§6.4).
type Problem struct {
	Rule     string // a Rule* constant
	Platform string // the platform it breaks on, or "" for both
	Context  Context
	Key      string
	Command  CommandID
	Message  string
}

func (p Problem) String() string {
	if p.Platform != "" {
		return p.Platform + ": " + p.Message
	}
	return p.Message
}

// The rules Validate checks.
const (
	RuleSyntax     = "syntax"            // a key string is not canonical notation
	RuleUnknown    = "unknown_command"   // a binding names no command
	RuleDupCommand = "duplicate_command" // two commands share an ID
	RuleContext    = "context"           // a binding is in a context its command doesn't run in
	RuleDuplicate  = "duplicate"         // a key is bound twice in one context
	RuleBlockKey   = "block_single_key"  // a block-selection command has no single key in block
	RuleReserved   = "reserved"          // a binding uses a key the browser keeps
	RulePalette    = "palette_only"      // a palette-only command has a key
	RuleUnbound    = "unbound"           // a command that isn't palette-only has no key
)

// Reserved lists the keys browsers keep for themselves in a normal tab
// (§6.4 "Browser rules"), in unresolved notation.
var Reserved = []string{"mod+w", "mod+t", "mod+n", "ctrl+tab", "mod+q"}

// Validate checks the rules of §6.4. An unresolved table is checked on both
// platforms; a problem found on both is reported once, without a platform.
func (t Table) Validate() []Problem {
	var out []Problem
	ids := map[CommandID]bool{}
	for _, c := range t.commands {
		if ids[c.ID] {
			out = append(out, Problem{Rule: RuleDupCommand, Command: c.ID, Message: fmt.Sprintf("command %s is defined twice", c.ID)})
		}
		ids[c.ID] = true
	}
	for _, b := range t.bindings {
		if _, _, err := ParseKey(b.Key); err != nil {
			out = append(out, Problem{Rule: RuleSyntax, Context: b.Context, Key: b.Key, Command: b.Command, Message: err.Error()})
		}
		c, ok := t.Command(b.Command)
		switch {
		case !ok:
			out = append(out, Problem{Rule: RuleUnknown, Context: b.Context, Key: b.Key, Command: b.Command,
				Message: fmt.Sprintf("%s in %s is bound to unknown command %q", b.Key, b.Context, b.Command)})
		case !slices.Contains(c.Contexts, b.Context):
			out = append(out, Problem{Rule: RuleContext, Context: b.Context, Key: b.Key, Command: b.Command,
				Message: fmt.Sprintf("%s is bound in %s, where %s doesn't run", b.Key, b.Context, b.Command)})
		case c.PaletteOnly:
			out = append(out, Problem{Rule: RulePalette, Context: b.Context, Key: b.Key, Command: b.Command,
				Message: fmt.Sprintf("palette-only command %s is bound to %s", b.Command, b.Key)})
		}
	}
	platforms := []string{Linux, MacOS}
	if t.platform != "" {
		platforms = []string{t.platform}
	}
	var per [][]Problem
	for _, p := range platforms {
		per = append(per, t.Resolve(p).validateResolved())
	}
	if len(per) == 1 {
		return append(out, per[0]...)
	}
	// Report a problem found on both platforms once.
	for _, p := range per[0] {
		if i := slices.IndexFunc(per[1], func(q Problem) bool { return sameProblem(p, q) }); i >= 0 {
			per[1] = slices.Delete(per[1], i, i+1)
			out = append(out, p)
			continue
		}
		p.Platform = platforms[0]
		out = append(out, p)
	}
	for _, p := range per[1] {
		p.Platform = platforms[1]
		out = append(out, p)
	}
	return out
}

func sameProblem(a, b Problem) bool {
	return a.Rule == b.Rule && a.Context == b.Context && a.Command == b.Command && a.Message == b.Message
}

// validateResolved checks the rules that depend on the resolved keys.
func (t Table) validateResolved() []Problem {
	var out []Problem
	type slot struct {
		ctx Context
		key string
	}
	seen := map[slot]CommandID{}
	reserved := map[string]bool{}
	for _, r := range Reserved {
		reserved[resolveKey(r, t.platform)] = true
	}
	bound := map[CommandID]bool{}
	for _, b := range t.bindings {
		bound[b.Command] = true
		if prev, ok := seen[slot{b.Context, b.Key}]; ok {
			out = append(out, Problem{Rule: RuleDuplicate, Context: b.Context, Key: b.Key, Command: b.Command,
				Message: fmt.Sprintf("%s in %s is bound to both %s and %s", b.orig, b.Context, prev, b.Command)})
		} else {
			seen[slot{b.Context, b.Key}] = b.Command
		}
		if reserved[b.Key] {
			out = append(out, Problem{Rule: RuleReserved, Context: b.Context, Key: b.Key, Command: b.Command,
				Message: fmt.Sprintf("%s uses %s, which the browser keeps", b.Command, b.orig)})
		}
	}
	for _, c := range t.commands {
		if !c.PaletteOnly && !bound[c.ID] {
			out = append(out, Problem{Rule: RuleUnbound, Command: c.ID,
				Message: fmt.Sprintf("%s has no key and isn't palette-only", c.ID)})
		}
		if !c.BlockSelectionCommand {
			continue
		}
		if !slices.ContainsFunc(t.bindings, func(b Binding) bool {
			return b.Command == c.ID && b.Context == Block && singleKey(b.Key)
		}) {
			out = append(out, Problem{Rule: RuleBlockKey, Context: Block, Command: c.ID,
				Message: fmt.Sprintf("block-selection command %s has no single key in block", c.ID)})
		}
	}
	return out
}
