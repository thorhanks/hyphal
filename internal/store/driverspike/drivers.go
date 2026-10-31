package driverspike

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"runtime"

	ncruces "github.com/ncruces/go-sqlite3"
	ncrucesdriver "github.com/ncruces/go-sqlite3/driver"
	modernc "modernc.org/sqlite"
)

// Driver is the part of a SQLite driver the spike compares.
type Driver struct {
	Name    string
	sqlName string
	// Backup copies the main database of conn into the file at dst.
	Backup func(ctx context.Context, conn *sql.Conn, dst string) error
	// Restore copies the database file at src into the main database of conn.
	Restore func(ctx context.Context, conn *sql.Conn, src string) error
}

// Drivers lists the candidates.
var Drivers = []Driver{
	{Name: "modernc", sqlName: "sqlite", Backup: moderncBackup, Restore: moderncRestore},
	{Name: "ncruces", sqlName: "sqlite3", Backup: ncrucesBackup, Restore: ncrucesRestore},
}

// Pragmas are the connection settings from CONTRACTS §4 and §8.6.
func Pragmas(busyMillis int) []string {
	p := []string{
		fmt.Sprintf("busy_timeout(%d)", busyMillis),
		"journal_mode(WAL)",
		"synchronous(FULL)",
		"foreign_keys(ON)",
	}
	if runtime.GOOS == "darwin" {
		p = append(p, "fullfsync(ON)")
	}
	return p
}

// DSN builds a file URI that applies the pragmas on every new connection.
// Both drivers accept the same _pragma and _txlock parameters.
func DSN(path string, busyMillis int) string {
	q := url.Values{}
	for _, p := range Pragmas(busyMillis) {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	return "file:" + path + "?" + q.Encode()
}

// Open opens path with the standard pragmas.
func (d Driver) Open(path string, busyMillis int) (*sql.DB, error) {
	return sql.Open(d.sqlName, DSN(path, busyMillis))
}

type moderncBackuper interface {
	NewBackup(dstURI string) (*modernc.Backup, error)
	NewRestore(srcURI string) (*modernc.Backup, error)
}

func moderncRun(conn *sql.Conn, start func(moderncBackuper) (*modernc.Backup, error)) error {
	return conn.Raw(func(dc any) error {
		c, ok := dc.(moderncBackuper)
		if !ok {
			return errors.New("modernc: driver conn has no backup API")
		}
		b, err := start(c)
		if err != nil {
			return err
		}
		_, stepErr := b.Step(-1)
		return errors.Join(stepErr, b.Finish())
	})
}

func moderncBackup(_ context.Context, conn *sql.Conn, dst string) error {
	return moderncRun(conn, func(c moderncBackuper) (*modernc.Backup, error) { return c.NewBackup(dst) })
}

func moderncRestore(_ context.Context, conn *sql.Conn, src string) error {
	return moderncRun(conn, func(c moderncBackuper) (*modernc.Backup, error) { return c.NewRestore(src) })
}

func ncrucesRaw(conn *sql.Conn, f func(*ncruces.Conn) error) error {
	return conn.Raw(func(dc any) error {
		c, ok := dc.(ncrucesdriver.Conn)
		if !ok {
			return errors.New("ncruces: driver conn has no Raw")
		}
		return f(c.Raw())
	})
}

func ncrucesBackup(_ context.Context, conn *sql.Conn, dst string) error {
	return ncrucesRaw(conn, func(c *ncruces.Conn) error { return c.Backup("main", "file:"+dst) })
}

func ncrucesRestore(_ context.Context, conn *sql.Conn, src string) error {
	return ncrucesRaw(conn, func(c *ncruces.Conn) error { return c.Restore("main", "file:"+src+"?mode=ro") })
}
