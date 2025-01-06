package pools

type Pools []Pool

type Pool struct {
	ID              string `mapstructure:"id"`
	Instance        string `mapstructure:"instance"`
	Name            string `mapstructure:"name"`
	Key             string `mapstructure:"key"`
	Exclude         bool   `mapstructure:"exclude"`
	AllowEmptySlots bool   `mapstructure:"allow-empty-slots"`
}

type PoolStats struct {
	Active   int
	Excluded int
	Total    int
}

func (p *Pools) GetExcludedPools() []Pool {
	var excludedPools []Pool
	for _, pool := range *p {
		if pool.Exclude {
			excludedPools = append(excludedPools, pool)
		}
	}
	return excludedPools
}

func (p *Pools) GetActivePools() []Pool {
	var activePools []Pool
	for _, pool := range *p {
		if !pool.Exclude {
			activePools = append(activePools, pool)
		}
	}
	return activePools
}

func (p *Pools) GetPoolStats() PoolStats {
	return PoolStats{
		Active:   len(p.GetActivePools()),
		Excluded: len(p.GetExcludedPools()),
		Total:    len(*p),
	}
}
