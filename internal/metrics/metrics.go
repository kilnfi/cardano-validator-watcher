package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const (
	namespace = "cardano_validator_watcher"
	// EpochLabel is the label carrying the epoch number on per-epoch block metrics.
	EpochLabel = "epoch"
)

// poolLabels returns the labels identifying a monitored pool.
func poolLabels() []string {
	return []string{"pool_name", "pool_id", "pool_instance"}
}

// poolEpochLabels returns the pool labels plus the epoch label.
func poolEpochLabels() []string {
	return append(poolLabels(), EpochLabel)
}

type Collection struct {
	ChainID                           *prometheus.GaugeVec
	EpochDuration                     prometheus.Gauge
	NetworkEpoch                      prometheus.Gauge
	NextEpochStartTime                prometheus.Gauge
	NetworkBlockHeight                prometheus.Gauge
	NetworkSlot                       prometheus.Gauge
	NetworkEpochSlot                  prometheus.Gauge
	NetworkTotalPools                 prometheus.Gauge
	NetworkCurrentEpochProposedBlocks prometheus.Gauge
	NetworkActiveStake                prometheus.Gauge
	RelaysPerPool                     *prometheus.GaugeVec
	PoolsPledgeMet                    *prometheus.GaugeVec
	PoolsSaturationLevel              *prometheus.GaugeVec
	PoolsDRepRegistered               *prometheus.GaugeVec
	PoolLiveStake                     *prometheus.GaugeVec
	PoolActiveStake                   *prometheus.GaugeVec
	PoolLiveDelegators                *prometheus.GaugeVec
	PoolLivePledge                    *prometheus.GaugeVec
	PoolDeclaredPledge                *prometheus.GaugeVec
	PoolExpectedLeaderSlots           *prometheus.GaugeVec
	PoolNoLeaderSlotProbability       *prometheus.GaugeVec
	PoolExcluded                      *prometheus.GaugeVec
	PoolBlocksMinted                  *prometheus.GaugeVec
	PoolLastEpochRewards              *prometheus.GaugeVec
	PoolLastEpochOperatorRewards      *prometheus.GaugeVec
	PoolLastEpochROA                  *prometheus.GaugeVec
	PoolLastRewardedEpoch             *prometheus.GaugeVec
	DependencyUp                      *prometheus.GaugeVec
	LastSuccess                       *prometheus.GaugeVec
	MonitoredValidatorsCount          *prometheus.GaugeVec
	MissedBlocks                      *prometheus.CounterVec
	ConsecutiveMissedBlocks           *prometheus.GaugeVec
	OrphanedBlocks                    *prometheus.CounterVec
	ValidatedBlocks                   *prometheus.CounterVec
	ExpectedBlocks                    *prometheus.GaugeVec
	LatestSlotProcessedByBlockWatcher prometheus.Gauge
	NextSlotLeader                    *prometheus.GaugeVec
	HealthStatus                      prometheus.Gauge
	CardanoNodeUp                     *prometheus.GaugeVec
	CardanoNodeActive                 *prometheus.GaugeVec
}

func NewCollection() *Collection {
	return &Collection{
		ChainID: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "chain_id",
				Help:      "Chain ID",
			},
			[]string{"chain_id"},
		),
		EpochDuration: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "epoch_duration",
				Help:      "Duration of an epoch in days",
			},
		),
		NetworkEpoch: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_epoch",
				Help:      "Current epoch number",
			},
		),
		NextEpochStartTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "next_epoch_start_time",
				Help:      "start time of the next epoch in seconds",
			},
		),
		NetworkBlockHeight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_block_height",
				Help:      "Latest known block height",
			},
		),
		NetworkSlot: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_slot",
				Help:      "Latest known slot",
			},
		),
		NetworkEpochSlot: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_epoch_slot",
				Help:      "Latest known epoch slot",
			},
		),
		NetworkTotalPools: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_pools",
				Help:      "Total number of pools in the network",
			},
		),
		NetworkCurrentEpochProposedBlocks: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_blocks_proposed_current_epoch",
				Help:      "Number of blocks proposed in the current epoch by the network",
			},
		),
		NetworkActiveStake: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "network_active_stake",
				Help:      "Total active stake in the network",
			},
		),
		RelaysPerPool: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_relays",
				Help:      "Count of relays associated with each pool",
			},
			poolLabels(),
		),
		PoolsPledgeMet: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_pledge_met",
				Help:      "Whether the pool has met its pledge requirements or not (0 or 1)",
			},
			poolLabels(),
		),
		PoolsSaturationLevel: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_saturation_level",
				Help:      "The current saturation level of the pool in percent",
			},
			poolLabels(),
		),
		PoolsDRepRegistered: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_drep_registered",
				Help:      "Whether the pool owner is registered to a DRep (0 or 1)",
			},
			poolLabels(),
		),
		PoolLiveStake: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_live_stake",
				Help:      "Current stake delegated to the pool in lovelace (updated in real time)",
			},
			poolLabels(),
		),
		PoolActiveStake: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_active_stake",
				Help:      "Stake of the pool in lovelace taken from the snapshot used for the current epoch",
			},
			poolLabels(),
		),
		PoolLiveDelegators: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_live_delegators",
				Help:      "Current number of delegators of the pool",
			},
			poolLabels(),
		),
		PoolLivePledge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_live_pledge",
				Help:      "Current stake of the pool owners in lovelace",
			},
			poolLabels(),
		),
		PoolDeclaredPledge: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_declared_pledge",
				Help:      "Pledge declared in the pool registration certificate in lovelace",
			},
			poolLabels(),
		),
		PoolExpectedLeaderSlots: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_expected_leader_slots",
				Help:      "Mean number of leader slots expected in the current epoch for the active stake of the pool",
			},
			poolLabels(),
		),
		PoolNoLeaderSlotProbability: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_no_leader_slot_probability",
				Help:      "Probability (0 to 1) for the pool to get no leader slot in the current epoch given its active stake",
			},
			poolLabels(),
		),
		PoolExcluded: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_excluded",
				Help:      "Whether the pool is excluded from block monitoring in the configuration (0 or 1)",
			},
			poolLabels(),
		),
		PoolBlocksMinted: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_blocks_minted",
				Help:      "Total number of blocks minted by the pool since its registration",
			},
			poolLabels(),
		),
		PoolLastEpochRewards: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_last_epoch_rewards",
				Help:      "Total rewards of the pool in lovelace (operator share included) for the last epoch with computed rewards",
			},
			poolLabels(),
		),
		PoolLastEpochOperatorRewards: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_last_epoch_operator_rewards",
				Help:      "Operator share (fixed cost and margin) of the rewards in lovelace for the last epoch with computed rewards",
			},
			poolLabels(),
		),
		PoolLastEpochROA: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_last_epoch_roa",
				Help:      "Annualized return of the delegators (ratio) for the last epoch with computed rewards",
			},
			poolLabels(),
		),
		PoolLastRewardedEpoch: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "pool_last_rewarded_epoch",
				Help:      "Epoch number of the rewards exposed by the pool_last_epoch_* metrics",
			},
			poolLabels(),
		),
		DependencyUp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "dependency_up",
				Help:      "Health of each dependency of the watcher (blockfrost, cardano_node): 1 = healthy, 0 = unhealthy",
			},
			[]string{"dependency"},
		),
		LastSuccess: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "last_success_timestamp_seconds",
				Help:      "Unix time of the last successful run of each component: an old value means its metrics are frozen",
			},
			[]string{"component"},
		),
		MonitoredValidatorsCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "monitored_validators",
				Help:      "number of validators monitored by the watcher",
			},
			[]string{"status"},
		),
		MissedBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "missed_blocks_total",
				Help:      "number of missed blocks in the current epoch",
			},
			poolEpochLabels(),
		),
		ConsecutiveMissedBlocks: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "consecutive_missed_blocks",
				Help:      "number of consecutive missed blocks in a row",
			},
			poolEpochLabels(),
		),
		ValidatedBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "validated_blocks_total",
				Help:      "number of validated blocks in the current epoch",
			},
			poolEpochLabels(),
		),
		OrphanedBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "orphaned_blocks_total",
				Help:      "number of orphaned blocks in the current epoch",
			},
			poolEpochLabels(),
		),
		ExpectedBlocks: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "expected_blocks",
				Help:      "number of expected blocks in the current epoch",
			},
			poolEpochLabels(),
		),
		LatestSlotProcessedByBlockWatcher: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "latest_slot_processed_by_block_watcher",
				Help:      "latest slot processed by the block watcher",
			},
		),
		NextSlotLeader: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "next_slot_leader",
				Help:      "next slot leader for each monitored pool",
			},
			poolEpochLabels(),
		),
		HealthStatus: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "health_status",
				Help:      "Health status of the Cardano validator watcher: 1 = healthy, 0 = unhealthy",
			},
		),
		CardanoNodeUp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "cardano_node_up",
				Help:      "Reachability of each configured cardano-node endpoint: 1 = reachable, 0 = down",
			},
			[]string{"remote"},
		),
		CardanoNodeActive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "cardano_node_active",
				Help:      "Cardano-node endpoint currently selected by the socket proxy: 1 = active, 0 = standby",
			},
			[]string{"remote"},
		),
	}
}

func (m *Collection) MustRegister(reg prometheus.Registerer) {
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(m.ChainID)
	reg.MustRegister(m.EpochDuration)
	reg.MustRegister(m.NetworkEpoch)
	reg.MustRegister(m.NetworkBlockHeight)
	reg.MustRegister(m.NetworkSlot)
	reg.MustRegister(m.NetworkEpochSlot)
	reg.MustRegister(m.NetworkTotalPools)
	reg.MustRegister(m.NetworkCurrentEpochProposedBlocks)
	reg.MustRegister(m.NetworkActiveStake)
	reg.MustRegister(m.RelaysPerPool)
	reg.MustRegister(m.NextEpochStartTime)
	reg.MustRegister(m.PoolsPledgeMet)
	reg.MustRegister(m.PoolsSaturationLevel)
	reg.MustRegister(m.PoolsDRepRegistered)
	reg.MustRegister(m.PoolLiveStake)
	reg.MustRegister(m.PoolActiveStake)
	reg.MustRegister(m.PoolLiveDelegators)
	reg.MustRegister(m.PoolLivePledge)
	reg.MustRegister(m.PoolDeclaredPledge)
	reg.MustRegister(m.PoolExpectedLeaderSlots)
	reg.MustRegister(m.PoolNoLeaderSlotProbability)
	reg.MustRegister(m.PoolExcluded)
	reg.MustRegister(m.PoolBlocksMinted)
	reg.MustRegister(m.PoolLastEpochRewards)
	reg.MustRegister(m.PoolLastEpochOperatorRewards)
	reg.MustRegister(m.PoolLastEpochROA)
	reg.MustRegister(m.PoolLastRewardedEpoch)
	reg.MustRegister(m.DependencyUp)
	reg.MustRegister(m.LastSuccess)
	reg.MustRegister(m.MonitoredValidatorsCount)
	reg.MustRegister(m.MissedBlocks)
	reg.MustRegister(m.ConsecutiveMissedBlocks)
	reg.MustRegister(m.ValidatedBlocks)
	reg.MustRegister(m.OrphanedBlocks)
	reg.MustRegister(m.ExpectedBlocks)
	reg.MustRegister(m.LatestSlotProcessedByBlockWatcher)
	reg.MustRegister(m.NextSlotLeader)
	reg.MustRegister(m.HealthStatus)
	reg.MustRegister(m.CardanoNodeUp)
	reg.MustRegister(m.CardanoNodeActive)
}

// DeleteEpochSeries removes the epoch-labelled gauge series of the given epoch.
// Counters are reset at each epoch transition, but gauges are not: without this,
// expected_blocks, consecutive_missed_blocks and next_slot_leader keep exporting
// a series for every epoch since the watcher started.
func (m *Collection) DeleteEpochSeries(epoch string) {
	labels := prometheus.Labels{EpochLabel: epoch}
	m.ExpectedBlocks.DeletePartialMatch(labels)
	m.ConsecutiveMissedBlocks.DeletePartialMatch(labels)
	m.NextSlotLeader.DeletePartialMatch(labels)
}
