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
	MonitoredValidatorsCount          *prometheus.GaugeVec
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
		MonitoredValidatorsCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "cardano_validator_watcher",
				Name:      "monitored_validators",
				Help:      "number of validators monitored by the watcher",
			},
			[]string{"status"},
		),
	}
}

func (m *Collection) MustRegister(reg prometheus.Registerer) {
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(m.ChainID)
	reg.MustRegister(m.EpochDuration)
	reg.MustRegister(m.NetworkEpoch)
	reg.MustRegister(m.NextEpochStartTime)
	reg.MustRegister(m.NetworkBlockHeight)
	reg.MustRegister(m.NetworkSlot)
	reg.MustRegister(m.NetworkEpochSlot)
	reg.MustRegister(m.NetworkTotalPools)
	reg.MustRegister(m.NetworkCurrentEpochProposedBlocks)
	reg.MustRegister(m.NetworkActiveStake)
	reg.MustRegister(m.RelaysPerPool)
	reg.MustRegister(m.PoolsPledgeMet)
	reg.MustRegister(m.PoolsSaturationLevel)
	reg.MustRegister(m.MonitoredValidatorsCount)
}
