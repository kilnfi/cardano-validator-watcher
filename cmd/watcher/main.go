package main

import (
	"log/slog"
	"os"

	"github.com/kilnfi/cardano-validator-watcher/cmd/watcher/app"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	command := app.NewWatcherCommand()
	if err := command.Execute(); err != nil {
		logger.Error("command execution failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
