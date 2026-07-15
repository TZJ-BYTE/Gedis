package database

import (
	"fmt"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

func (db *Database) GetStringBytesCopy(key []byte) ([]byte, bool, error) {
	k := bytesToString(key)
	shard := db.getShard(k)

	shard.lock.RLock()
	dv, exists := shard.data[k]
	if !exists || dv == nil {
		shard.lock.RUnlock()
		return nil, false, nil
	}
	now := time.Now().UnixMilli()
	if dv.ExpireTime > 0 && now > dv.ExpireTime {
		shard.lock.RUnlock()
		return nil, false, nil
	}
	switch v := dv.Value.(type) {
	case *datastruct.String:
		out := []byte(v.Data)
		shard.lock.RUnlock()
		return out, true, nil
	case *datastruct.BytesString:
		out := make([]byte, len(v.Data))
		copy(out, v.Data)
		shard.lock.RUnlock()
		return out, true, nil
	default:
		shard.lock.RUnlock()
		return nil, false, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
}

func (db *Database) GetStringCopy(key []byte) (string, bool, error) {
	b, ok, err := db.GetStringBytesCopy(key)
	if !ok || err != nil {
		return "", ok, err
	}
	return protocol.BytesToString(b), true, nil
}

