package slotleader

import (
	"fmt"
)

type ErrSlotLeaderRefresh struct {
	PoolID  string
	Epoch   int
	Message string
}

func (e *ErrSlotLeaderRefresh) Error() string {
	return fmt.Sprintf("unable to refresh slot leaders for pool %s at epoch %d: %s", e.PoolID, e.Epoch, e.Message)
}
