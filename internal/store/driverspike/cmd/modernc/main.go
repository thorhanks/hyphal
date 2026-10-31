// Command modernc opens a database with the modernc driver, for binary size and
// cross-compilation checks (P0-05 items 6–7).
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var v string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(v)
}
