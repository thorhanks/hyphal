package fracindex

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// tb is the part of testing.TB that *rapid.T also implements.
type tb interface {
	Helper()
	Fatalf(format string, args ...any)
}

func mustBetween(t tb, a, b string) string {
	t.Helper()
	k, err := Between(a, b)
	if err != nil {
		t.Fatalf("Between(%q, %q): %v", a, b, err)
	}
	return k
}

// checkBetween fails unless k is a valid key strictly inside (a, b), where an
// empty bound is open.
func checkBetween(t tb, a, b, k string) {
	t.Helper()
	if !Valid(k) {
		t.Fatalf("Between(%q, %q) = %q, not a valid key", a, b, k)
	}
	if (a != "" && k <= a) || (b != "" && k >= b) {
		t.Fatalf("Between(%q, %q) = %q, not strictly between", a, b, k)
	}
}

func TestBetween_OpenEnds(t *testing.T) {
	if got := mustBetween(t, "", ""); got != "a0" {
		t.Fatalf(`Between("", "") = %q, want "a0"`, got)
	}
	for _, k := range []string{"a0", "a1", "Zz", "zzzzzzzzzzzzzzzzzzzzzzzzzzz", "A00000000000000000000000001", "a0V", "b10"} {
		checkBetween(t, k, "", mustBetween(t, k, ""))
		checkBetween(t, "", k, mustBetween(t, "", k))
	}
}

func TestBetween_Examples(t *testing.T) {
	// Cases from the published algorithm's test suite.
	tests := []struct{ a, b, want string }{
		{"", "", "a0"},
		{"", "a0", "Zz"},
		{"", "Zz", "Zy"},
		{"a0", "", "a1"},
		{"a1", "", "a2"},
		{"a0", "a1", "a0V"},
		{"a1", "a2", "a1V"},
		{"a0V", "a1", "a0l"},
		{"Zz", "a0", "ZzV"},
		{"Zz", "a1", "a0"},
		{"", "Y00", "Xzzz"},
		{"bzz", "", "c000"},
		{"a0", "a0V", "a0G"},
		{"a0", "a0G", "a08"},
		{"b125", "b129", "b127"},
		{"a0", "a1V", "a1"},
		{"Zz", "a01", "a0"},
		{"", "a0V", "a0"},
		{"", "b999", "b99"},
		{"", "A00000000000000000000000000V", "A00000000000000000000000000G"},
		{"", "A000000000000000000000000001", "A000000000000000000000000000V"},
		{"", "A00000000000000000000000001", "A00000000000000000000000000V"},
		{"zzzzzzzzzzzzzzzzzzzzzzzzzzy", "", "zzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"zzzzzzzzzzzzzzzzzzzzzzzzzzz", "", "zzzzzzzzzzzzzzzzzzzzzzzzzzzV"},
	}
	for _, tt := range tests {
		if got := mustBetween(t, tt.a, tt.b); got != tt.want {
			t.Errorf("Between(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestBetween_Errors(t *testing.T) {
	tests := []struct {
		a, b string
		want error
	}{
		{"a0", "a0", ErrOrder},
		{"a1", "a0", ErrOrder},
		{"a00", "", ErrInvalidKey},
		{"a00", "a1", ErrInvalidKey},
		{"0", "1", ErrInvalidKey},
		{"a1", "2", ErrInvalidKey},
		{"", "A00000000000000000000000000", ErrInvalidKey},
		{"a", "", ErrInvalidKey},
		{"a0-", "", ErrInvalidKey},
	}
	for _, tt := range tests {
		if _, err := Between(tt.a, tt.b); !errors.Is(err, tt.want) {
			t.Errorf("Between(%q, %q) error = %v, want %v", tt.a, tt.b, err, tt.want)
		}
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"a0", true},
		{"a0V", true},
		{"Zz", true},
		{"b00", true},
		{"zzzzzzzzzzzzzzzzzzzzzzzzzzz", true},
		{"A000000000000000000000000000V", true},
		{"", false},
		{"a", false},
		{"b0", false},
		{"a0V0", false},
		{"a00", false},
		{"A00000000000000000000000000", false},
		{"0a", false},
		{"a0!", false},
		{"a0é", false},
	}
	for _, tt := range tests {
		if got := Valid(tt.in); got != tt.want {
			t.Errorf("Valid(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// keyGen draws valid keys of any shape, including the extreme heads.
func keyGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		digit := rapid.SampledFrom([]byte(digits))
		for {
			var sb strings.Builder
			head := rapid.SampledFrom([]byte("ABCXYZabcxyz"+digits[10:])).Draw(t, "head")
			sb.WriteByte(head)
			for range integerLength(head) - 1 {
				sb.WriteByte(digit.Draw(t, "int"))
			}
			for range rapid.IntRange(0, 4).Draw(t, "fracLen") {
				sb.WriteByte(digit.Draw(t, "frac"))
			}
			k := strings.TrimRight(sb.String(), "0")
			if len(k) < integerLength(head) {
				k += strings.Repeat("0", integerLength(head)-len(k))
			}
			if Valid(k) {
				return k
			}
		}
	})
}

func TestBetween_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := keyGen().Draw(t, "a")
		b := keyGen().Draw(t, "b")
		if a == b {
			t.Skip("equal keys")
		}
		if a > b {
			a, b = b, a
		}
		checkBetween(t, a, b, mustBetween(t, a, b))
		checkBetween(t, a, "", mustBetween(t, a, ""))
		checkBetween(t, "", b, mustBetween(t, "", b))
	})
}

func FuzzBetween(f *testing.F) {
	for _, s := range [][2]string{
		{"", ""}, {"a0", ""}, {"", "a0"}, {"a0", "a1"}, {"Zz", "a0"}, {"a0V", "a1"},
		{"", "A000000000000000000000000001"}, {"", "A00000000000000000000000001"}, {"zzzzzzzzzzzzzzzzzzzzzzzzzzz", ""},
		{"a00", "a1"}, {"a1", "a0"},
	} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		k, err := Between(a, b)
		wantErr := (a != "" && !Valid(a)) || (b != "" && !Valid(b)) || (a != "" && b != "" && a >= b)
		if wantErr {
			if err == nil {
				t.Fatalf("Between(%q, %q) = %q, want error", a, b, k)
			}
			return
		}
		if err != nil {
			t.Fatalf("Between(%q, %q): %v", a, b, err)
		}
		checkBetween(t, a, b, k)
	})
}

func TestBetween_LengthGrowth(t *testing.T) {
	const n = 10_000
	first := mustBetween(t, "", "")
	last := first
	maxLen := len(first)
	for range n {
		k := mustBetween(t, "", first)
		checkBetween(t, "", first, k)
		first, maxLen = k, max(maxLen, len(k))
	}
	for range n {
		k := mustBetween(t, last, "")
		checkBetween(t, last, "", k)
		last, maxLen = k, max(maxLen, len(k))
	}
	t.Logf("max key length after %d front and %d back inserts: %d bytes (first %q, last %q)", n, n, maxLen, first, last)
	if maxLen > 20 {
		t.Fatalf("max key length %d, want <= 20", maxLen)
	}
}

func TestNBetween(t *testing.T) {
	bounds := [][2]string{{"", ""}, {"a0", ""}, {"", "a0"}, {"a0", "a1"}, {"a0", "a0V"}, {"Zz", "a0"}}
	for _, bd := range bounds {
		for _, n := range []int{0, 1, 2, 3, 10, 100, 1000} {
			keys, err := NBetween(bd[0], bd[1], n)
			if err != nil {
				t.Fatalf("NBetween(%q, %q, %d): %v", bd[0], bd[1], n, err)
			}
			if len(keys) != n {
				t.Fatalf("NBetween(%q, %q, %d) returned %d keys", bd[0], bd[1], n, len(keys))
			}
			prev := bd[0]
			for _, k := range keys {
				checkBetween(t, prev, bd[1], k)
				prev = k
			}
		}
	}
	if _, err := NBetween("a1", "a0", 3); !errors.Is(err, ErrOrder) {
		t.Errorf("NBetween with a > b: error = %v, want ErrOrder", err)
	}
	if _, err := NBetween("", "", -1); err == nil {
		t.Error("NBetween with n < 0: want error")
	}
}

func TestNBetween_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := keyGen().Draw(t, "a")
		b := keyGen().Draw(t, "b")
		if a == b {
			t.Skip("equal keys")
		}
		if a > b {
			a, b = b, a
		}
		n := rapid.IntRange(0, 50).Draw(t, "n")
		keys, err := NBetween(a, b, n)
		if err != nil {
			t.Fatalf("NBetween(%q, %q, %d): %v", a, b, n, err)
		}
		if len(keys) != n {
			t.Fatalf("NBetween(%q, %q, %d) returned %d keys", a, b, n, len(keys))
		}
		prev := a
		for _, k := range keys {
			checkBetween(t, prev, b, k)
			prev = k
		}
	})
}

// TestOrderIsByteOrder checks that string order equals byte order, which is
// SQLite's BINARY collation. store.TestOrderMatchesSQLite checks the real column.
func TestOrderIsByteOrder(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	keys := []string{mustBetween(t, "", "")}
	for len(keys) < 10_000 {
		i := r.IntN(len(keys) + 1)
		var a, b string
		if i > 0 {
			a = keys[i-1]
		}
		if i < len(keys) {
			b = keys[i]
		}
		keys = slices.Insert(keys, i, mustBetween(t, a, b))
	}
	shuffled := slices.Clone(keys)
	r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	slices.SortFunc(shuffled, func(x, y string) int { return bytes.Compare([]byte(x), []byte(y)) })
	if !slices.Equal(shuffled, keys) {
		t.Fatal("byte order differs from insertion order")
	}
}
