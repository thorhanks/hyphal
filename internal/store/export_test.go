package store

import (
	"context"
	"fmt"
	"strings"
)

// ExecForTest writes rows no public call writes yet: view_state and ui_state.
func (db *DB) ExecForTest(query string, args ...any) error {
	_, err := db.sql.Exec(query, args...)
	return err
}

// RowsForTest returns every row of each table with its column types, so NULL,
// empty text and 0 stay distinct. The last_* meta keys describe this file, not
// its content, and are left out (A-006).
func (db *DB) RowsForTest(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	for _, table := range []string{"meta", "nodes", "pages", "stars", "view_state", "ui_state", "change_sets", "ops"} {
		rows, err := db.sql.QueryContext(ctx, `SELECT * FROM `+table+` ORDER BY 1`)
		if err != nil {
			return nil, err
		}
		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if key, ok := values[0].(string); ok && table == "meta" && strings.HasPrefix(key, "last_") {
				continue
			}
			out[table] = append(out[table], fmt.Sprintf("%#v", values))
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
