// Command ncruces opens a database with the ncruces driver, for binary size and
// cross-compilation checks (P0-05 items 6–7).
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func main() {
	db, err := sql.Open("sqlite3", "file::memory:")
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
