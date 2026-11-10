package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
)

// TODO(P1-01): config owns platform paths (§7.1); these move there.

// DataDir is the data directory (CONTRACTS §9).
func DataDir() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "hyphal"), nil
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "hyphal"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "hyphal"), nil
}

// DefaultDB is the database path from HYPHAL_DB, else <data>/outline.db (§10.2).
func DefaultDB() (string, error) {
	if p := os.Getenv("HYPHAL_DB"); p != "" {
		return filepath.Abs(p)
	}
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "outline.db"), nil
}

// RuntimeDir holds server state files: ${XDG_RUNTIME_DIR}/hyphal, else
// <data>/tmp (CONTRACTS §9).
func RuntimeDir(dataDir string) string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "hyphal")
	}
	return filepath.Join(dataDir, "tmp")
}

// StateFile is the server state file for a database (CONTRACTS §9, D-041).
func StateFile(runtimeDir, dbPath string) string {
	sum := sha256.Sum256([]byte(dbPath))
	return filepath.Join(runtimeDir, "server-"+hex.EncodeToString(sum[:])[:16]+".json")
}

// Stable ports (§9.3, D-047): the first free one of 16 from 40000 plus the
// FNV-1a hash of the absolute database path mod 10000.
const (
	portBase  = 40000
	portSpan  = 10000
	portTries = 16
)

func firstPort(dbPath string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(dbPath))
	return portBase + int(h.Sum32()%portSpan)
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

var errNoPort = errors.New("no free port: tried 16 from the database's stable port (§9.3)")
