// Package driverspike compares modernc.org/sqlite and
// github.com/ncruces/go-sqlite3 for card P0-05 (§9.1, §8.5, §9.4, §12.4.7).
//
// It is a separate Go module so the losing driver never enters the main
// go.mod. Run `go test -v ./...` here for the behavior checks and
// `go test -run '^$' -bench . ./...` (or `./measure.sh`) for the numbers.
// Results: plan/reports/p0-05-sqlite-driver.md.
package driverspike
