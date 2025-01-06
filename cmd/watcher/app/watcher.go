package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kilnfi/cardano-validator-watcher/cmd/watcher/app/config"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost/blockfrostapi"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/kilnfi/cardano-validator-watcher/internal/server/http"
	"github.com/kilnfi/cardano-validator-watcher/internal/watcher"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	configFile string
	server     *http.Server
	cfg        *config.Config
	logger     *slog.Logger
)

func init() {
	cobra.OnInitialize(initLogger)
	cobra.OnInitialize(loadConfig)
}

func initLogger() {
	var logLevel slog.Level
	switch viper.GetString("log-level") {
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	case "debug":
		logLevel = slog.LevelDebug
	default:
		logLevel = slog.LevelInfo
	}

	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
}

func NewWatcherCommand() *cobra.Command {
	cmd := &cobra.Command{
		TraverseChildren: true,
		Use:              "cardano-validator-watcher",
		Short:            "cardano validator watcher is used to monitor our cardano pools",
		Long: `cardano validator watcher is a long-running program designed
		to collect metrics for monitoring our Cardano validation nodes.
		This tool helps us ensure the health and performance of our nodes in the Cardano network.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          run,
	}

	cmd.Flags().StringVarP(&configFile, "config", "", "", "config file (default is config.yml)")
	cmd.Flags().StringP("log-level", "", "info", "config file (default is config.yml)")
	cmd.Flags().StringP("http-server-host", "", http.ServerDefaultHost, "host on which HTTP server should listen")
	cmd.Flags().IntP("http-server-port", "", http.ServerDefaultPort, "port on which HTTP server should listen")
	cmd.Flags().StringP("network", "", "preprod", "cardano network ID")
	cmd.Flags().StringP("blockfrost-project-id", "", "", "blockfrost project id")
	cmd.Flags().StringP("blockfrost-endpoint", "", "", "blockfrost API endpoint")
	cmd.Flags().IntP("blockfrost-max-routines", "", 10, "number of routines used by blockfrost to perform concurrent actions")
	cmd.Flags().IntP("blockfrost-timeout", "", 60, "Timeout for requests to the Blockfrost API (in seconds)")
	cmd.Flags().BoolP("pool-watcher-enabled", "", true, "Enable pool watcher")
	cmd.Flags().IntP("pool-watcher-refresh-interval", "", 60, "Interval at which the pool watcher collects data about the monitored pools (in seconds)")

	// bind flag to viper
	checkError(viper.BindPFlag("log-level", cmd.Flag("log-level")), "unable to bind log-level flag")
	checkError(viper.BindPFlag("http.host", cmd.Flag("http-server-host")), "unable to bind http-server-host flag")
	checkError(viper.BindPFlag("http.port", cmd.Flag("http-server-port")), "unable to bind http-server-port flag")
	checkError(viper.BindPFlag("network", cmd.Flag("network")), "unable to bind network flag")
	checkError(viper.BindPFlag("blockfrost.project-id", cmd.Flag("blockfrost-project-id")), "unable to bind blockfrost-project-id flag")
	checkError(viper.BindPFlag("blockfrost.endpoint", cmd.Flag("blockfrost-endpoint")), "unable to bind blockfrost-endpoint flag")
	checkError(viper.BindPFlag("blockfrost.max-routines", cmd.Flag("blockfrost-max-routines")), "unable to bind blockfrost-max-routines flag")
	checkError(viper.BindPFlag("blockfrost.timeout", cmd.Flag("blockfrost-timeout")), "unable to bind blockfrost-timeout flag")
	checkError(viper.BindPFlag("pool-watcher.enabled", cmd.Flag("pool-watcher-enabled")), "unable to bind pool-watcher-enabled flag")
	checkError(viper.BindPFlag("pool-watcher.refresh-interval", cmd.Flag("pool-watcher-refresh-interval")), "unable to bind pool-watcher-refresh-interval flag")

	return cmd
}

// loadConfig read the configuration and load it.
func loadConfig() {
	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	// read the config file
	if err := viper.ReadInConfig(); err != nil {
		logger.Error("unable to read config file", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// unmarshal the config
	cfg = &config.Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		logger.Error("unable to unmarshal config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// validate the config
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(_ *cobra.Command, _ []string) error {
	// Initialize context and cancel function
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize signal channel for handling interrupts
	ctx, cancel = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	eg, ctx := errgroup.WithContext(ctx)

	// Initialize blockfrost and cardano clients with options
	blockfrost := createBlockfrostClient()

	// Initialize prometheus metrics
	registry := prometheus.NewRegistry()
	metrics := metrics.NewCollection()
	metrics.MustRegister(registry)

	// Start HTTP server
	if err := startHTTPServer(eg, registry); err != nil {
		return fmt.Errorf("unable to start http server: %w", err)
	}

	// Start Pool Watcher
	if cfg.PoolWatcherConfig.Enabled {
		startPoolWatcher(ctx, eg, blockfrost, metrics, cfg.Pools)
	}

	<-ctx.Done()
	logger.Info("shutting down")

	// shutting down HTTP server
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger.Info("stopping http server")
	if err := server.Stop(ctx); err != nil {
		logger.Error("unable to stop http service", slog.String("error", err.Error()))
	}

	if err := eg.Wait(); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info("Program interrupted by user")
			return nil
		}
		return fmt.Errorf("error during execution: %w", err)
	}
	return nil
}

func createBlockfrostClient() blockfrost.Client {
	opts := blockfrostapi.ClientOptions{
		ProjectID:   cfg.Blockfrost.ProjectID,
		Server:      cfg.Blockfrost.Endpoint,
		MaxRoutines: cfg.Blockfrost.MaxRoutines,
		Timeout:     time.Second * time.Duration(cfg.Blockfrost.Timeout),
	}
	return blockfrostapi.NewClient(opts)
}

func startHTTPServer(eg *errgroup.Group, registry *prometheus.Registry) error {
	var err error

	server, err = http.New(
		registry,
		http.WithHost(cfg.HTTP.Host),
		http.WithPort(cfg.HTTP.Port),
	)
	if err != nil {
		return fmt.Errorf("unable to create http server: %w", err)
	}

	eg.Go(func() error {
		logger.Info(
			"starting http server",
			slog.String("component", "http-server"),
			slog.String("addr", fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)),
		)
		if err := server.Start(); err != nil {
			return fmt.Errorf("unable to start http server: %w", err)
		}
		return nil
	})

	return nil
}

// startPoolWatcher starts the pool watcher service
func startPoolWatcher(
	ctx context.Context,
	eg *errgroup.Group,
	blockfrost blockfrost.Client,
	metrics *metrics.Collection,
	pools pools.Pools,
) {
	eg.Go(func() error {
		options := watcher.PoolWatcherOptions{
			RefreshInterval: time.Second * time.Duration(cfg.PoolWatcherConfig.RefreshInterval),
			Network:         cfg.Network,
		}
		logger.Info(
			"starting watcher",
			slog.String("component", "pool-watcher"),
		)
		poolWatcher, err := watcher.NewPoolWatcher(blockfrost, metrics, pools, options)
		if err != nil {
			return fmt.Errorf("unable to create pool watcher: %w", err)
		}
		if err := poolWatcher.Start(ctx); err != nil {
			return fmt.Errorf("unable to start pool watcher: %w", err)
		}
		return nil
	})
}

// checkError is a helper function to log an error and exit the program
// used for the flag parsing
func checkError(err error, msg string) {
	if err != nil {
		logger.Error(msg, slog.String("error", err.Error()))
		os.Exit(1)
	}
}
