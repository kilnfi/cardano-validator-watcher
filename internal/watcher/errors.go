package watcher

import (
	"errors"
	"fmt"
)

var (
	ErrBlockFrostAPINotReachable = errors.New("blockfrost API is not reachable")
	ErrCardanoNodeNotReachable   = errors.New("cardano node is not reachable")
)

type ErrNoSlotsAssignedToPool struct {
	PoolID string
	Epoch  int
}

func (e *ErrNoSlotsAssignedToPool) Error() string {
	return fmt.Sprintf("Pool %s has no slots assigned for epoch %d. Consider excluding this pool.", e.PoolID, e.Epoch)
}
