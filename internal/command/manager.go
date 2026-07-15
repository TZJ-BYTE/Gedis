package command

import (
	"sync/atomic"

	"github.com/TZJ-BYTE/RediGo/internal/database"
)

var globalDBManager atomic.Pointer[database.DBManager]

func SetDBManager(m *database.DBManager) {
	globalDBManager.Store(m)
}

func getDBManager() *database.DBManager {
	return globalDBManager.Load()
}
