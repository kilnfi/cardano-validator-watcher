package config

import (
	"errors"
	"fmt"

	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

type Config struct {
	Pools                pools.Pools          `mapstructure:"pools"`
	HTTP                 HTTPConfig           `mapstructure:"http"`
	Network              string               `mapstructure:"network"`
	Blockfrost           BlockFrostConfig     `mapstructure:"blockfrost"`
	PoolWatcherConfig    PoolWatcherConfig    `mapstructure:"pool-watcher"`
	NetworkWatcherConfig NetworkWatcherConfig `mapstructure:"network-watcher"`
}

type PoolWatcherConfig struct {
	Enabled         bool `mapstructure:"enabled"`
	RefreshInterval int  `mapstructure:"refresh-interval"`
}

type NetworkWatcherConfig struct {
	Enabled         bool `mapstructure:"enabled"`
	RefreshInterval int  `mapstructure:"refresh-interval"`
}

type HTTPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type BlockFrostConfig struct {
	ProjectID   string `mapstructure:"project-id"`
	Endpoint    string `mapstructure:"endpoint"`
	MaxRoutines int    `mapstructure:"max-routines"`
	Timeout     int    `mapstructure:"timeout"`
}

func (c *Config) Validate() error {
	switch c.Network {
	case "mainnet", "preprod":
	default:
		return fmt.Errorf("invalid network: %s. Network must be either %s or %s", c.Network, "mainnet", "preprod")
	}

	if len(c.Pools) == 0 {
		return errors.New("at least one pool must be defined")
	}
	for _, pool := range c.Pools {
		if pool.Instance == "" {
			return errors.New("instance is required for all pools")
		}
		if pool.ID == "" {
			return errors.New("id is required for all pools")
		}
		if pool.Name == "" {
			return errors.New("name is required for all pools")
		}
		if pool.Key == "" {
			return errors.New("key is required for all pools")
		}
	}

	activePools := c.Pools.GetActivePools()
	if len(activePools) == 0 {
		return errors.New("at least one active pool must be defined")
	}

	if c.Blockfrost.ProjectID == "" || c.Blockfrost.Endpoint == "" {
		return errors.New("blockfrost project-id and endpoint are required")
	}

	return nil
}
