package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thorhanks/hyphal/internal/platform"
)

// Auth lifetimes (§9.3, D-041, D-047).
const (
	ticketLife = 30 * time.Second
	cookieLife = 365 * 24 * time.Hour
)

// csp is sent on every response (CONTRACTS §12.4).
const csp = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// loadKey reads <data>/session.key, creating it on first use (D-047). The
// cookie is signed with it, so cookies survive restarts.
func loadKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "session.key")
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		return b, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := ensureDir(dataDir); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	err = platform.WriteFileAtomic(path, 0o600, func(w io.Writer) error {
		_, err := w.Write(key)
		return err
	})
	return key, err
}

// auth issues single-use launch tickets and signs session cookies (§9.3, D-041).
type auth struct {
	key    []byte
	secret string // the state file's secret, which buys tickets
	port   int
	clock  func() time.Time

	mu      sync.Mutex
	tickets map[string]time.Time // ticket → expiry
}

func (a *auth) cookieName() string { return "hyphal_" + strconv.Itoa(a.port) }

// newTicket returns a ticket that works once within ticketLife.
func (a *auth) newTicket() string {
	t := randomHex(16)
	now := a.clock()
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, exp := range a.tickets {
		if now.After(exp) {
			delete(a.tickets, k)
		}
	}
	a.tickets[t] = now.Add(ticketLife)
	return t
}

// useTicket consumes a ticket and reports whether it was valid.
func (a *auth) useTicket(t string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.tickets[t]
	delete(a.tickets, t)
	return ok && !a.clock().After(exp)
}

func (a *auth) secretOK(s string) bool {
	return subtle.ConstantTimeCompare([]byte(s), []byte(a.secret)) == 1
}

func (a *auth) sign(msg string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(a.cookieName() + "|" + msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// cookieValue is "<issued unix seconds>.<signature>".
func (a *auth) cookieValue() string {
	issued := strconv.FormatInt(a.clock().Unix(), 10)
	return issued + "." + a.sign(issued)
}

func (a *auth) cookieOK(v string) bool {
	issued, sig, ok := strings.Cut(v, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(a.sign(issued))) {
		return false
	}
	sec, err := strconv.ParseInt(issued, 10, 64)
	if err != nil {
		return false
	}
	age := a.clock().Sub(time.Unix(sec, 0))
	return age >= -time.Minute && age < cookieLife
}
