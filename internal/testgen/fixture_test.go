package testgen

import (
	"math/rand/v2"
	"testing"

	"github.com/thorhanks/hyphal/internal/core"
)

func TestFixture_RoundTrip(t *testing.T) {
	examples := []string{
		"- Projects {zoom}\n  - ControllerCraft {collapsed}\n    - Development\n  - Fix tool‸tip\n  - ⟦Selected⟧ text\n  - [x] Done task\n  - ## Heading {page}\n  - Picked {sel}",
		"- ‸Alpha\n- Beta",
		"- \n- ‸Alpha\n- Beta",
		"- [ ] ## Both\n- [ ] Task",
		"- Note {page=hello}\n- Starred {star}\n- Gone {trashed}\n  - Child {trashed}",
		"- Fixed {id=k3j9x2m4q7tb}",
		"- ‸⟦from start⟧",
		"- a {zoom collapsed star sel}",
		"- {zoom}",
		"- literal {braces} here\n- ends {notattr}",
	}
	for _, s := range examples {
		t.Run(s, func(t *testing.T) {
			f := MustParseOutline(t, s)
			Equal(t, s, FormatOutline(f.Tree, f.State))
		})
	}
}

func TestFixture_State(t *testing.T) {
	f := MustParseOutline(t, "- ab⟦cd⟧e\n- Z {sel}")
	id := f.ID(t, "abcde")
	if f.State.SelNode != id || f.State.SelStart != 2 || f.State.SelEnd != 4 || f.State.Cursor != id || f.State.CursorOff != 4 {
		t.Errorf("state: %+v", f.State)
	}
	if n, _ := f.Tree.Node(id); n.Text != "abcde" {
		t.Errorf("text %q", n.Text)
	}
	f = MustParseOutline(t, "- é‸x")
	if f.State.CursorOff != 2 {
		t.Errorf("offset %d, want bytes", f.State.CursorOff)
	}
}

func TestFixture_Errors(t *testing.T) {
	for _, s := range []string{"no bullet", " - odd", "- a\n    - too far", "  - starts deep"} {
		if _, err := ParseOutline(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
}

func TestFixture_TreeIsValid(t *testing.T) {
	f := MustParseOutline(t, "- Projects\n  - A {page}\n  - [x] B {star}\n  - C {trashed}\n    - D {trashed}")
	if p := f.Tree.Check(); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
}

func TestOutline_IsValid(t *testing.T) {
	for seed := range uint64(30) {
		d := Outline(rand.New(rand.NewPCG(seed, 1)), OutlineOpts{Nodes: 200})
		if p := core.NewTree(d).Check(); len(p) != 0 {
			t.Fatalf("seed %d: %v", seed, p)
		}
		if len(d.Nodes) != 201 {
			t.Fatalf("%d nodes", len(d.Nodes))
		}
	}
}
