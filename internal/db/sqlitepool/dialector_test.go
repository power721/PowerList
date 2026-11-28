//go:build !sqlite_cgo_compat && !(linux && (mips || mips64 || mips64le || mipsle || loong64)) && !(windows && 386)

package sqlitepool

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	gosqlite "github.com/glebarez/go-sqlite"
	"gorm.io/gorm"
	"modernc.org/sqlite/lib"
)

func TestSQLiteFailedCommitClearsTransaction(t *testing.T) {
	ctx := t.Context()
	// In rollback-journal mode a reader prevents a writer's COMMIT. Disable
	// the normal busy wait so the test doesn't depend on scheduling or sleeps.
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(0)"
	gormDB, err := gorm.Open(Open(dsn))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(3)

	if _, err := db.ExecContext(ctx, "CREATE TABLE items (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	reader := sqliteTestConn(t, ctx, db)
	writer := sqliteTestConn(t, ctx, db)
	readTx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	var count int
	if err := readTx.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}

	writeTx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writeTx.Rollback()
	if _, err := writeTx.ExecContext(ctx, "INSERT INTO items (id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	commitErr := writeTx.Commit()
	var sqliteErr *gosqlite.Error
	if !errors.As(commitErr, &sqliteErr) || sqliteErr.Code() != sqlite3.SQLITE_BUSY {
		t.Fatalf("expected original SQLITE_BUSY from commit, got %v", commitErr)
	}

	// A third connection must be able to read while the original reader is
	// still active. A failed (not rolled back) COMMIT blocks this new reader.
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatalf("new reader still blocked after failed commit: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed write was not rolled back: got %d rows", count)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Reuse the same physical writer connection. The failed transaction must
	// not cause a nested-transaction error or block subsequent writes.
	nextTx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("writer was not reusable: %v", err)
	}
	defer nextTx.Rollback()
	if _, err := nextTx.ExecContext(ctx, "INSERT INTO items (id) VALUES (2)"); err != nil {
		t.Fatal(err)
	}
	if err := nextTx.Commit(); err != nil {
		t.Fatalf("subsequent write did not commit: %v", err)
	}
	var id int
	if err := db.QueryRowContext(ctx, "SELECT id FROM items").Scan(&id); err != nil || id != 2 {
		t.Fatalf("expected only the subsequent write to persist, got id=%d, err=%v", id, err)
	}
}

func sqliteTestConn(t *testing.T, ctx context.Context, db *sql.DB) *sql.Conn {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestSQLiteFailedRollbackInvalidatesConnection(t *testing.T) {
	commitErr := errors.New("commit failed")
	rollbackErr := errors.New("rollback failed")
	conn := &sqliteRollbackConn{}
	tx := sqliteRollbackTx{Tx: sqliteFailingTx{commitErr, rollbackErr}, conn: conn}

	err := tx.Commit()
	if !errors.Is(err, commitErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("expected both errors to be preserved, got %v", err)
	}
	if conn.IsValid() {
		t.Fatal("an unclean connection must not be returned to the pool")
	}
}

type sqliteFailingTx struct{ commitErr, rollbackErr error }

func (tx sqliteFailingTx) Commit() error   { return tx.commitErr }
func (tx sqliteFailingTx) Rollback() error { return tx.rollbackErr }
