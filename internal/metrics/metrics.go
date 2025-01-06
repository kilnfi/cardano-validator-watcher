package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type Collection struct {
	RelaysPerPool            *prometheus.GaugeVec
	PoolsPledgeMet           *prometheus.GaugeVec
	PoolsSaturationLevel     *prometheus.GaugeVec
	MonitoredValidatorsCount *prometheus.GaugeVec
}

func NewCollection() *Collection {
	return &Collection{
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
	reg.MustRegister(m.RelaysPerPool)
	reg.MustRegister(m.PoolsPledgeMet)
	reg.MustRegister(m.PoolsSaturationLevel)
	reg.MustRegister(m.MonitoredValidatorsCount)
}
