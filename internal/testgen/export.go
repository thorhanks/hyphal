package testgen

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/thorhanks/hyphal/internal/model"
	"github.com/thorhanks/hyphal/internal/store"
)

// ExportJSON writes a dump in the JSON export format (CONTRACTS §6), so a
// fixture can be loaded into a real database with store.ImportJSON. The
// servertest harness and the dev server's --seed use it.
func ExportJSON(d *store.Dump) ([]byte, error) {
	type node struct {
		ID          model.NodeID       `json:"id"`
		ParentID    *model.NodeID      `json:"parent_id"`
		Position    string             `json:"position"`
		Text        string             `json:"text"`
		IsTask      bool               `json:"is_task"`
		IsHeading   bool               `json:"is_heading"`
		CompletedAt *model.Time        `json:"completed_at"`
		CreatedAt   model.Time         `json:"created_at"`
		UpdatedAt   model.Time         `json:"updated_at"`
		Version     int64              `json:"version"`
		DeletedAt   *model.Time        `json:"deleted_at"`
		DeletedBy   *model.ChangeSetID `json:"deleted_by"`
	}
	type page struct {
		NodeID    model.NodeID `json:"node_id"`
		Markdown  string       `json:"markdown"`
		UpdatedAt model.Time   `json:"updated_at"`
		Version   int64        `json:"version"`
	}
	type star struct {
		NodeID    model.NodeID `json:"node_id"`
		StarredAt model.Time   `json:"starred_at"`
	}
	type view struct {
		NodeID    model.NodeID `json:"node_id"`
		Collapsed bool         `json:"collapsed"`
	}
	type ui struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	out := struct {
		Format        string            `json:"format"`
		FormatVersion int               `json:"format_version"`
		SchemaVersion int               `json:"schema_version"`
		ExportedBy    string            `json:"exported_by"`
		ExportedAt    model.Time        `json:"exported_at"`
		Meta          map[string]string `json:"meta"`
		Nodes         []node            `json:"nodes"`
		Pages         []page            `json:"pages"`
		Stars         []star            `json:"stars"`
		ViewState     []view            `json:"view_state"`
		UIState       []ui              `json:"ui_state"`
	}{
		Format: "hyphal-export", FormatVersion: 1, SchemaVersion: 1,
		ExportedBy: "testgen", ExportedAt: model.NewTime(FixtureTime), Meta: map[string]string{"format_id": "tree-outline"},
		Nodes: []node{}, Pages: []page{}, Stars: []star{}, ViewState: []view{}, UIState: []ui{},
	}
	for k, v := range d.Meta {
		out.Meta[k] = v
	}
	for _, n := range d.Nodes {
		e := node{
			ID: n.ID, Position: n.Position, Text: n.Text, IsTask: n.IsTask, IsHeading: n.IsHeading,
			CompletedAt: n.CompletedAt, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, Version: n.Version,
			DeletedAt: n.DeletedAt,
		}
		if n.ParentID != "" {
			e.ParentID = &n.ParentID
		}
		if n.DeletedBy != "" {
			e.DeletedBy = &n.DeletedBy
		}
		out.Nodes = append(out.Nodes, e)
	}
	slices.SortFunc(out.Nodes, func(a, b node) int { return strings.Compare(string(a.ID), string(b.ID)) })
	for id, p := range d.Pages {
		out.Pages = append(out.Pages, page{id, p.Markdown, p.UpdatedAt, p.Version})
	}
	slices.SortFunc(out.Pages, func(a, b page) int { return strings.Compare(string(a.NodeID), string(b.NodeID)) })
	for id, at := range d.Stars {
		out.Stars = append(out.Stars, star{id, at})
	}
	slices.SortFunc(out.Stars, func(a, b star) int { return strings.Compare(string(a.NodeID), string(b.NodeID)) })
	for id, c := range d.Collapsed {
		if c {
			out.ViewState = append(out.ViewState, view{id, true})
		}
	}
	slices.SortFunc(out.ViewState, func(a, b view) int { return strings.Compare(string(a.NodeID), string(b.NodeID)) })
	for k, v := range d.UIState {
		out.UIState = append(out.UIState, ui{k, v})
	}
	slices.SortFunc(out.UIState, func(a, b ui) int { return strings.Compare(a.Key, b.Key) })
	return json.Marshal(out)
}
