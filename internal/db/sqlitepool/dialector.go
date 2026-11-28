//go:build !sqlite_cgo_compat && !(linux && (mips || mips64 || mips64le || mipsle || loong64)) && !(windows && 386)

package sqlitepool

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const sqliteRollbackDriverName = "openlist-sqlite-rollback"

func init() {
	sql.Register(sqliteRollbackDriverName, sqliteRollbackDriver{base: &gosqlite.Driver{}})
}

// Open returns the pure-Go SQLite dialector with failed-commit cleanup.
func Open(dsn string) gorm.Dialector {
	return sqlite.Dialector{DriverName: sqliteRollbackDriverName, DSN: dsn}
}

// Preserve the driver's context-aware statement, query and transaction paths.
// Embedding only driver.Conn would hide these optional interfaces from sql.DB.
type sqliteContextConn interface {
	driver.Conn
	driver.ConnPrepareContext
	driver.ConnBeginTx
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
}

type sqliteRollbackDriver struct{ base driver.Driver }

func (d sqliteRollbackDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := d.base.Open(dsn)
	if err != nil {
		return nil, err
	}
	contextConn, ok := conn.(sqliteContextConn)
	if !ok {
		return nil, errors.Join(errors.New("SQLite driver lacks context-aware connection methods"), conn.Close())
	}
	return &sqliteRollbackConn{sqliteContextConn: contextConn}, nil
}

type sqliteRollbackConn struct {
	sqliteContextConn
	discard bool
}

func (c *sqliteRollbackConn) IsValid() bool { return !c.discard }

func (c *sqliteRollbackConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *sqliteRollbackConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.sqliteContextConn.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return sqliteRollbackTx{Tx: tx, conn: c}, nil
}

type sqliteRollbackTx struct {
	driver.Tx
	conn *sqliteRollbackConn
}

func (tx sqliteRollbackTx) Commit() error {
	err := tx.Tx.Commit()
	if err == nil {
		return nil
	}

	// SQLite can leave the transaction active after a failed COMMIT (e.g.
	// SQLITE_BUSY). database/sql has already finished its Tx at this point,
	// so cleanup must happen on the driver transaction, not on *sql.Tx.
	if rollbackErr := tx.Tx.Rollback(); rollbackErr != nil {
		// Evict an unclean connection without turning the commit error into a
		// retry signal. Returning it to the pool may retain the write lock.
		tx.conn.discard = true
		return errors.Join(err, fmt.Errorf("roll back after failed SQLite commit: %w", rollbackErr))
	}
	return err
}
