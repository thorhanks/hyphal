package testgen

import (
	"bytes"
	"context"
	"testing"

	"github.com/thorhanks/hyphal/internal/core"
	"github.com/thorhanks/hyphal/internal/store"
)

func TestExportJSON_ImportsTheFixture(t *testing.T) {
	const want = "- Alpha {collapsed}\n  - [x] Kid {star}\n- ## Beta {page=notes}\n- Gone {trashed}"
	f := MustParseOutline(t, want)
	b, err := ExportJSON(f.Dump)
	if err != nil {
		t.Fatal(err)
	}
	db := TempDB(t)
	ctx := context.Background()
	if err := db.ImportJSON(ctx, bytes.NewReader(b), store.ImportReplace); err != nil {
		t.Fatal(err)
	}
	d, err := db.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tr := core.NewTree(d)
	pages, err := db.LoadPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tr.SetPages(pages)
	Equal(t, want, FormatOutline(tr, State{}))
}
