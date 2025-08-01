package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

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
	MonitoredValidatorsCount          *prometheus.GaugeVec
	MissedBlocks                      *prometheus.CounterVec
	ConsecutiveMissedBlocks           *prometheus.GaugeVec
	OrphanedBlocks                    *prometheus.CounterVec
	ValidatedBlocks                   *prometheus.CounterVec
	ExpectedBlocks                    *prometheus.GaugeVec
	LatestSlotProcessedByBlockWatcher prometheus.Gauge
	NextSlotLeader                    *prometheus.GaugeVec
	HealthStatus                      prometheus.Gauge
}

func NewCollection() *Collection {
	return &Collection{
		ChainID: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "chain_id",
				Help:      "Chain ID",
			},
			[]string{"chain_id"},
		),
		EpochDuration: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "epoch_duration",
				Help:      "Duration of an epoch in days",
			},
		),
		NetworkEpoch: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_epoch",
				Help:      "Current epoch number",
			},
		),
		NextEpochStartTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "next_epoch_start_time",
				Help:      "start time of the next epoch in seconds",
			},
		),
		NetworkBlockHeight: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_block_height",
				Help:      "Latest known block height",
			},
		),
		NetworkSlot: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_slot",
				Help:      "Latest known slot",
			},
		),
		NetworkEpochSlot: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_epoch_slot",
				Help:      "Latest known epoch slot",
			},
		),
		NetworkTotalPools: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_pools",
				Help:      "Total number of pools in the network",
			},
		),
		NetworkCurrentEpochProposedBlocks: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_blocks_proposed_current_epoch",
				Help:      "Number of blocks proposed in the current epoch by the network",
			},
		),
		NetworkActiveStake: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "network_active_stake",
				Help:      "Total active stake in the network",
			},
		),
		RelaysPerPool: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "pool_relays",
				Help:      "Count of relays associated with each pool",
			},
			[]string{"pool_name", "pool_id", "pool_instance"},
		),
		PoolsPledgeMet: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "pool_pledge_met",
				Help:      "Whether the pool has met its pledge requirements or not (0 or 1)",
			},
			[]string{"pool_name", "pool_id", "pool_instance"},
		),
		PoolsSaturationLevel: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "pool_saturation_level",
				Help:      "The current saturation level of the pool in percent",
			},
			[]string{"pool_name", "pool_id", "pool_instance"},
		),
		PoolsDRepRegistered: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "pool_drep_registered",
				Help:      "Whether the pool owner is registered to a DRep (0 or 1)",
			},
			[]string{"pool_name", "pool_id", "pool_instance"},
		),
		MonitoredValidatorsCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "monitored_validators",
				Help:      "number of validators monitored by the watcher",
			},
			[]string{"status"},
		),
		MissedBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "missed_blocks_total",
				Help:      "number of missed blocks in the current epoch",
			},
			[]string{"pool_name", "pool_id", "pool_instance", "epoch"},
		),
		ConsecutiveMissedBlocks: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "consecutive_missed_blocks",
				Help:      "number of consecutive missed blocks in a row",
			},
			[]string{"pool_name", "pool_id", "pool_instance", "epoch"},
		),
		ValidatedBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "validated_blocks_total",
				Help:      "number of validated blocks in the current epoch",
			},
			[]string{"pool_name", "pool_id", "pool_instance", "epoch"},
		),
		OrphanedBlocks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "orphaned_blocks_total",
				Help:      "number of orphaned blocks in the current epoch",
			},
			[]string{"pool_name", "pool_id", "pool_instance", "epoch"},
		),
		ExpectedBlocks: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "expected_blocks",
				Help:      "number of expected blocks in the current epoch",
			},
			[]string{"pool_name", "pool_id", "pool_instance", "epoch"},
		),
		LatestSlotProcessedByBlockWatcher: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "latest_slot_processed_by_block_watcher",
				Help:      "latest slot processed by the block watcher",
			},
		),
		NextSlotLeader: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "next_slot_leader",
				Help:      "next slot leader for each monitored pool",
			},
			[]string{"pool_name", "pool_id", "pool_instance", "epoch"},
		),
		HealthStatus: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "health_status",
				Help:      "Health status of the Cardano validator watcher: 1 = healthy, 0 = unhealthy",
			},
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
	reg.MustRegister(m.MonitoredValidatorsCount)
	reg.MustRegister(m.MissedBlocks)
	reg.MustRegister(m.ConsecutiveMissedBlocks)
	reg.MustRegister(m.ValidatedBlocks)
	reg.MustRegister(m.OrphanedBlocks)
	reg.MustRegister(m.ExpectedBlocks)
	reg.MustRegister(m.LatestSlotProcessedByBlockWatcher)
	reg.MustRegister(m.NextSlotLeader)
	reg.MustRegister(m.HealthStatus)
}
