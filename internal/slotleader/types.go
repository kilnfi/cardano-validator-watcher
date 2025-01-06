package slotleader

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	bfAPI "github.com/blockfrost/blockfrost-go"
	"github.com/jmoiron/sqlx"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

type SlotLeader interface {
	Refresh(ctx context.Context, epoch bfAPI.Epoch) error
	IsSlotLeader(ctx context.Context, PoolID string, slot int, epoch int) (bool, error)
	IsSlotsEmpty(ctx context.Context, PoolID string, epoch int) (bool, error)
	GetSlotLeaders(ctx context.Context, PoolID string, epoch int) (Schedule, error)
	GetNextSlotLeader(ctx context.Context, PoolID string, height int, epoch int) (int, error)
}

type Service struct {
	db         *sqlx.DB
	logger     *slog.Logger
	pools      pools.Pools
	cardano    cardano.CardanoClient
	blockfrost blockfrost.Client
	metrics    *metrics.Collection
}

type slots []int
type Schedule struct {
	ID       int    `db:"id"`
	Epoch    int    `db:"epoch"`
	PoolID   string `db:"pool_id"`
	Quantity int    `db:"slot_qty"`
	Slots    slots  `db:"slots"`
	Hash     string `db:"hash"`
}

// Scan implements the sql.Scanner interface to convert
// the text representation of the slots to a slice of integers
//
//nolint:wrapcheck
func (s *slots) Scan(value interface{}) error {
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	default:
		return errors.New("type assertion to []byte or string failed")
	}
}
