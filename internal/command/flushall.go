package command

import (
	"errors"

	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

type FlushAllCommand struct{}

func (c *FlushAllCommand) Execute(_ *database.Database, args [][]byte) *protocol.Response {
	if len(args) != 0 {
		return protocol.MakeError(errors.New("ERR wrong number of arguments for 'flushall' command"))
	}
	m := getDBManager()
	if m == nil {
		return protocol.MakeError(errors.New("ERR DB manager not available"))
	}
	if err := m.FlushAll(); err != nil {
		return protocol.MakeError(err)
	}
	return protocol.MakeSimpleString("OK")
}
