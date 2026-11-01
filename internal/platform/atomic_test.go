package platform

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	writeString := func(s string) func(io.Writer) error {
		return func(w io.Writer) error {
			_, err := io.WriteString(w, s)
			return err
		}
	}
	entries := func(t *testing.T, dir string) int {
		t.Helper()
		list, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}

	t.Run("writes the file with the mode", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "export.json")
		if err := WriteFileAtomic(path, 0o600, writeString("new")); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "new" {
			t.Errorf("content %q, %v", got, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, %v", info.Mode(), err)
		}
		if n := entries(t, dir); n != 1 {
			t.Errorf("%d files in the directory", n)
		}
	})

	failing := func(w io.Writer) error {
		if _, err := io.WriteString(w, "partial"); err != nil {
			return err
		}
		return errors.New("injected failure")
	}

	t.Run("failure leaves no file", func(t *testing.T) {
		dir := t.TempDir()
		err := WriteFileAtomic(filepath.Join(dir, "export.json"), 0o600, failing)
		if err == nil {
			t.Fatal("no error")
		}
		if n := entries(t, dir); n != 0 {
			t.Errorf("%d files left behind", n)
		}
	})

	t.Run("failure keeps the old file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "export.json")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := WriteFileAtomic(path, 0o600, failing); err == nil {
			t.Fatal("no error")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "old" {
			t.Errorf("content %q, %v", got, err)
		}
		if n := entries(t, dir); n != 1 {
			t.Errorf("%d files in the directory", n)
		}
	})

	t.Run("replaces an existing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "export.json")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteFileAtomic(path, 0o600, writeString("new")); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "new" {
			t.Errorf("content %q, %v", got, err)
		}
	})
}

func TestIgnoreUnsupported(t *testing.T) {
	for _, err := range []error{nil, syscall.EINVAL, syscall.ENOTSUP} {
		if got := ignoreUnsupported(err); got != nil {
			t.Errorf("%v: got %v", err, got)
		}
	}
	if got := ignoreUnsupported(syscall.EIO); !errors.Is(got, syscall.EIO) {
		t.Errorf("EIO: got %v", got)
	}
}
