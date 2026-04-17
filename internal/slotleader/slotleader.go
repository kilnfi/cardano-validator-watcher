package slotleader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

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
	concurrency int,
) *Service {
	logger := slog.With(
		slog.String("component", "slot-leader-service"),
	)

	return &Service{
		db:          db,
		logger:      logger,
		pools:       pools,
		cardano:     cardanocli,
		blockfrost:  blockfrost,
		metrics:     metrics,
		concurrency: concurrency,
	}
}

func (s *Service) RefreshCurrent(ctx context.Context, epoch bfAPI.Epoch) error {
	return s.refresh(ctx, epoch, "current", s.concurrency)
}

func (s *Service) RefreshNext(ctx context.Context, epoch bfAPI.Epoch, concurrency int) error {
	return s.refresh(ctx, epoch, "next", concurrency)
}

//nolint:wrapcheck
func (s *Service) refresh(ctx context.Context, epoch bfAPI.Epoch, ledgerSet string, concurrency int) error {
	eg := errgroup.Group{}
	if concurrency > 0 {
		eg.SetLimit(concurrency)
	}

	activePools := s.pools.GetActivePools()
	if concurrency > 0 {
		s.logger.InfoContext(ctx, "🔄 refreshing slot leaders",
			slog.Int("pools", len(activePools)),
			slog.Int("concurrency", concurrency),
			slog.Int("epoch", epoch.Epoch),
		)
	} else {
		s.logger.InfoContext(ctx, "🔄 refreshing slot leaders",
			slog.Int("pools", len(activePools)),
			slog.String("concurrency", "unlimited"),
			slog.Int("epoch", epoch.Epoch),
		)
	}

	epochParams, err := s.blockfrost.GetEpochParameters(ctx, epoch.Epoch)
	if err != nil {
		return fmt.Errorf("slotLeader: unable to get epoch parameters: %w", err)
	}

	for _, pool := range activePools {
		eg.Go(func(pool pools.Pool) func() error {
			return func() error {
				refreshed, err := s.isRefresh(ctx, pool.ID, epoch.Epoch)
				if err != nil {
					return fmt.Errorf("unable to check if slots are already refreshed for %s: %w", pool.Name, err)
				}

				if !refreshed {
					s.logger.InfoContext(ctx,
						fmt.Sprintf("⏰ refreshing slots for pool: %s", pool.Name),
						slog.String("pool_id", pool.ID),
					)
					response, err := s.cardano.LeaderLogs(ctx, ledgerSet, epochParams.Nonce, pool)
					if err != nil {
						return &ErrSlotLeaderRefresh{PoolID: pool.ID, Epoch: epoch.Epoch, Message: err.Error()}
					}
					if err := s.persistSlots(ctx, pool.ID, epoch.Epoch, response); err != nil {
						return fmt.Errorf("unable to persist slots for pool %s: %w", pool.Name, err)
					}
					s.logger.InfoContext(ctx,
						fmt.Sprintf("✅ slots persisted for pool: %s", pool.Name),
						slog.String("pool_id", pool.ID),
						slog.Int("epoch", epoch.Epoch),
						slog.Int("slot_count", len(response.AssignedSlots)),
					)
				} else {
					s.logger.InfoContext(ctx,
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

func (s *Service) RunNextEpochScheduler(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			epoch, err := s.blockfrost.GetLatestEpoch(ctx)
			if err != nil {
				s.logger.ErrorContext(ctx, "🚨 next-epoch-scheduler: unable to get latest epoch",
					slog.String("error", err.Error()),
				)
				continue
			}

			timeUntilEnd := time.Until(time.Unix(int64(epoch.EndTime), 0).UTC())
			s.logger.DebugContext(ctx, "🔍 next-epoch-scheduler: checking epoch end time",
				slog.Int("current_epoch", epoch.Epoch),
				slog.String("time_until_end", timeUntilEnd.Round(time.Minute).String()),
			)

			if timeUntilEnd > 24*time.Hour {
				continue
			}

			nextEpoch := bfAPI.Epoch{Epoch: epoch.Epoch + 1}
			s.logger.InfoContext(ctx, "⏩ pre-computing next epoch slot schedule",
				slog.Int("next_epoch", nextEpoch.Epoch),
				slog.String("next_epoch_in", timeUntilEnd.Round(time.Minute).String()),
			)
			if err := s.RefreshNext(ctx, nextEpoch, s.concurrency); err != nil {
				s.logger.ErrorContext(ctx, "🚨 next-epoch-scheduler: unable to pre-compute next epoch",
					slog.Int("next_epoch", nextEpoch.Epoch),
					slog.String("error", err.Error()),
				)
				continue
			}
			s.logger.InfoContext(ctx, "✅ next epoch slot schedule pre-computed successfully",
				slog.Int("next_epoch", nextEpoch.Epoch),
			)
		}
	}
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
	s.logger.DebugContext(ctx,
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

func (s *Service) persistSlots(ctx context.Context, poolID string, epoch int, response cardano.ClientLeaderLogsResponse) error {
	assignedSlots := make([]int, len(response.AssignedSlots))
	for i, slot := range response.AssignedSlots {
		assignedSlots[i] = slot.Slot
	}

	slotsJSON, err := json.Marshal(assignedSlots)
	if err != nil {
		return fmt.Errorf("unable to marshal slots: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO slots (epoch, pool_id, slot_qty, slots, hash) VALUES (?, ?, ?, ?, ?)`,
		epoch, poolID, len(assignedSlots), string(slotsJSON), "",
	)
	return err
}
