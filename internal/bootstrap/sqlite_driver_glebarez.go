//go:build !sqlite_cgo_compat && !(linux && (mips || mips64 || mips64le || mipsle || loong64)) && !(windows && 386)

package bootstrap

import (
	"github.com/OpenListTeam/OpenList/v4/internal/db/sqlitepool"
	"gorm.io/gorm"
)

func openSQLite(dsn string) gorm.Dialector {
	return sqlitepool.Open(dsn)
}
