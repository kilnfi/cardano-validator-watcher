package slotleader

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"

	bfAPI "github.com/blockfrost/blockfrost-go"
	"github.com/jmoiron/sqlx"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"golang.org/x/sync/errgroup"
)

var _ SlotLeader = (*Service)(nil)

func NewSlotLeaderService(
	db *sqlx.DB,
	cardanocli cardano.CardanoClient,
	blockfrost blockfrost.Client,
	pools pools.Pools,
	metrics *metrics.Collection,
) *Service {
	logger := slog.With(
		slog.String("component", "slot-leader-service"),
	)

	return &Service{
		db:         db,
		logger:     logger,
		pools:      pools,
		cardano:    cardanocli,
		blockfrost: blockfrost,
		metrics:    metrics,
	}
}

//nolint:wrapcheck
func (s *Service) Refresh(ctx context.Context, epoch bfAPI.Epoch) error {
	eg := errgroup.Group{}

	epochParams, err := s.blockfrost.GetEpochParameters(ctx, epoch.Epoch)
	if err != nil {
		return fmt.Errorf("slotLeader: unable to get epoch parameters: %w", err)
	}

	for _, pool := range s.pools.GetActivePools() {
		eg.Go(func(pool pools.Pool) func() error {
			return func() error {
				// check if we have already refreshed the slots for this pool
				refreshed, err := s.isRefresh(ctx, pool.ID, epoch.Epoch)
				if err != nil {
					return fmt.Errorf("unable to check if slots are already refreshed for %s: %w", pool.Name, err)
				}

				if !refreshed {
					s.logger.Info(
						fmt.Sprintf("⏰ refreshing slots for pool: %s", pool.Name),
						slog.String("pool_id", pool.ID),
					)
					if err := s.cardano.LeaderLogs(ctx, "current", epochParams.Nonce, pool); err != nil {
						return &ErrSlotLeaderRefresh{PoolID: pool.ID, Epoch: epoch.Epoch, Message: err.Error()}
					}
				} else {
					s.logger.Info(
						fmt.Sprintf("💠 slots already refreshed for pool: %s", pool.Name),
						slog.String("pool_id", pool.ID),
					)
				}

				schedule, err := s.GetSlotLeaders(ctx, pool.ID, epoch.Epoch)
				if err != nil {
					return fmt.Errorf("unable to get slot leaders for pool %s: %w", pool.Name, err)
				}
				s.metrics.ExpectedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch.Epoch)).Set(float64(schedule.Quantity))
				return nil
			}
		}(pool))
	}
	return eg.Wait()
}

func (s *Service) GetSlotLeaders(ctx context.Context, PoolID string, epoch int) (Schedule, error) {
	schedule := []Schedule{}
	err := s.db.SelectContext(ctx, &schedule, `SELECT * FROM slots WHERE pool_id = ? AND epoch = ?`, PoolID, epoch)
	if err != nil {
		return Schedule{}, fmt.Errorf("GetSlotLeaders: unable to get slot leaders for pool %s: %w", PoolID, err)
	}

	if len(schedule) == 0 {
		return Schedule{}, fmt.Errorf("GetSlotLeaders: no slot leaders found for pool %s in epoch %d", PoolID, epoch)
	}

	return schedule[0], nil
}

func (s *Service) GetNextSlotLeader(ctx context.Context, PoolID string, height int, epoch int) (int, error) {
	schedule, err := s.GetSlotLeaders(ctx, PoolID, epoch)
	if err != nil {
		return 0, fmt.Errorf("GetNextSlotLeader: unable to get slot leaders for pool %s: %w", PoolID, err)
	}

	lastSlot := schedule.Slots[len(schedule.Slots)-1]
	s.logger.Debug(
		fmt.Sprintf("last slot for pool %s is %d", PoolID, lastSlot),
		slog.String("pool_id", PoolID),
		slog.Int("last_slot", lastSlot),
		slog.Int("height", height),
	)
	for _, slot := range schedule.Slots {
		if slot > height {
			return slot, nil
		}
	}

	return 0, nil
}

func (s *Service) isRefresh(ctx context.Context, PoolID string, epoch int) (bool, error) {
	schedule := []Schedule{}
	err := s.db.SelectContext(ctx, &schedule, `SELECT * FROM slots WHERE pool_id = ? AND epoch = ?`, PoolID, epoch)
	if err != nil {
		return false, fmt.Errorf("isRefresh: unable to check if slots are already refreshed for pool %s: %w", PoolID, err)
	}

	return len(schedule) > 0, nil
}

func (s *Service) IsSlotLeader(ctx context.Context, PoolID string, slot int, epoch int) (bool, error) {
	schedule := []Schedule{}
	err := s.db.SelectContext(ctx, &schedule, `SELECT slots FROM slots WHERE pool_id = ? AND epoch = ?`, PoolID, epoch)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("IsSlotLeader: unable to check if slot %d is a leader for pool %s: %w", slot, PoolID, err)
	}
	for _, sslot := range schedule[0].Slots {
		if sslot == slot {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) IsSlotsEmpty(ctx context.Context, PoolID string, epoch int) (bool, error) {
	schedule := []Schedule{}
	err := s.db.SelectContext(ctx, &schedule, `SELECT slots, slot_qty FROM slots WHERE pool_id = ? AND epoch = ?`, PoolID, epoch)
	if err != nil {
		return false, fmt.Errorf("IsSlotsEmpty: unable to check if slots are empty for pool %s: %w", PoolID, err)
	}

	return len(schedule[0].Slots) == 0 || schedule[0].Quantity == 0, nil
}
