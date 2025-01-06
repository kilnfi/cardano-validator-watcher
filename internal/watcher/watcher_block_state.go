package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
)

type BlockWatcherState struct {
	db         *sqlx.DB
	blockfrost blockfrost.Client
	logger     *slog.Logger

	Epoch      int       `db:"epoch"`
	Slot       int       `db:"slot"`
	LastUpdate time.Time `db:"last_update"`
}

func NewBlockWatcherState(db *sqlx.DB, blockfrost blockfrost.Client) *BlockWatcherState {
	logger := slog.With(
		slog.String("component", "block-watcher-state"),
	)
	return &BlockWatcherState{
		db:         db,
		blockfrost: blockfrost,
		logger:     logger,
	}
}

func (s *BlockWatcherState) LoadAndReconcile(ctx context.Context, currentEpoch int) error {
	if err := s.Load(ctx); err != nil {
		return fmt.Errorf("failed to load block watcher state: %w", err)
	}

	s.logger.Debug("current epoch", slog.Int("current_epoch", currentEpoch))
	s.logger.Debug("state epoch", slog.Int("state_epoch", s.Epoch))
	if currentEpoch > s.Epoch {
		block, err := s.blockfrost.GetFirstBlockInEpoch(ctx, currentEpoch)
		if err != nil {
			return fmt.Errorf("failed to get first block in epoch %d: %w", currentEpoch, err)
		}
		s.Epoch = currentEpoch
		s.Slot = block.Slot

		if err := s.Save(ctx, s.Epoch, s.Slot); err != nil {
			return fmt.Errorf("failed to save block watcher state (epoch: %d, slot: %d) after reconcile operation: %w", s.Epoch, s.Slot, err)
		}
	}

	return nil
}

func (s *BlockWatcherState) Load(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := "SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1"
	err := s.db.GetContext(cctx, s, query)
	if err != nil {
		if err == sql.ErrNoRows {
			return s.loadFromBlockfrost(ctx)
		}
		return fmt.Errorf("failed to execute SQL query while loading watcher state: %w", err)
	}
	return nil
}

func (s *BlockWatcherState) Save(ctx context.Context, epoch int, slot int) error {
	s.Epoch = epoch
	s.Slot = slot
	s.LastUpdate = time.Now()

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := "INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)"
	_, err := s.db.ExecContext(cctx, query, s.Epoch, s.Slot, s.LastUpdate)
	if err != nil {
		return fmt.Errorf("failed to execute SQL query while saving watcher state: %w", err)
	}
	return nil
}

func (s *BlockWatcherState) loadFromBlockfrost(ctx context.Context) error {
	block, err := s.blockfrost.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block from blockfrost: %w", err)
	}
	s.Epoch = block.Epoch
	s.Slot = block.Slot
	return nil
}
