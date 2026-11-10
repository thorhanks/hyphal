package store

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestSnapshot_Verified(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	at := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	db, err := Open(filepath.Join(dir, "outline.db"), OpenOptions{Create: true, Clock: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	name, err := db.Snapshot(ctx, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if name != "outline-20261110T093000Z-daily.db" {
		t.Errorf("name = %q", name)
	}
	snapDir := filepath.Join(dir, "snapshots")
	entries, err := os.ReadDir(snapDir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if !slices.Equal(files, []string{name}) {
		t.Errorf("snapshot directory holds %v, want only the snapshot", files)
	}
	info, err := os.Stat(filepath.Join(snapDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}

	// The copy opens on its own, without -wal or -shm files.
	snap, err := Open(filepath.Join(snapDir, name), OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	if err := quickCheck(ctx, snap.sql); err != nil {
		t.Error(err)
	}
	var mode string
	if err := snap.sql.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "delete" {
		t.Errorf("journal_mode = %q, %v; want delete", mode, err)
	}

	list, err := ListSnapshots(snapDir)
	if err != nil {
		t.Fatal(err)
	}
	want := SnapshotInfo{Name: name, Size: info.Size(), CreatedAt: at, Reason: "daily"}
	if len(list) != 1 || list[0] != want {
		t.Errorf("ListSnapshots = %+v, want %+v", list, want)
	}
}

func TestSnapshot_UnknownReason(t *testing.T) {
	db := openNew(t, filepath.Join(t.TempDir(), "outline.db"))
	if _, err := db.Snapshot(context.Background(), "weekly"); err == nil {
		t.Error("an unknown reason was accepted")
	}
}

func TestListSnapshots_SkipsPartialAndForeignFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"outline-20261110T093000Z-daily.db",
		"outline-20261111T093000Z-pre-import.db",
		"outline-20261112T093000Z-daily.db.tmp",
		"outline-20261112T093000Z-daily.db.tmp-journal",
		"outline-2026-daily.db",
		"outline-20261112T093000Z-weekly.db",
		"notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ListSnapshots(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range list {
		got = append(got, s.Name)
	}
	want := []string{"outline-20261111T093000Z-pre-import.db", "outline-20261110T093000Z-daily.db"}
	if !slices.Equal(got, want) {
		t.Errorf("ListSnapshots = %v, want %v", got, want)
	}
	if list, err := ListSnapshots(filepath.Join(dir, "missing")); err != nil || list != nil {
		t.Errorf("missing directory: %v, %v", list, err)
	}
}
