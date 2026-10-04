package handlers

import (
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// rowScanner is the shared shape of *sql.Row and *sql.Rows, so a scan helper
// can serve both the single-row and the multi-row path.
type rowScanner interface {
	Scan(dest ...any) error
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint
// violation, detected by the driver result code rather than the error text.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
