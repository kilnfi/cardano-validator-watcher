package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kilnfi/cardano-validator-watcher/internal/watcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	DefaultReadTimeout  = 15 * time.Second
	DefaultWriteTimeout = 30 * time.Second
	DefaultHost         = "0.0.0.0"
	DefaultPort         = 8000
)

func TestNewServer(t *testing.T) {
	t.Parallel()

	t.Run("WithOptions", func(t *testing.T) {
		t.Parallel()

		healthStore := watcher.NewHealthStore()
		server, err := New(
			nil,
			healthStore,
			WithHost(DefaultHost),
			WithPort(DefaultPort),
			WithReadTimeout(DefaultReadTimeout),
			WithWriteTimeout(DefaultWriteTimeout),
		)

		require.NoError(t, err)
		assert.Equal(t, DefaultHost, server.options.host)
		assert.Equal(t, DefaultPort, server.options.port)
		assert.Equal(t, DefaultReadTimeout, server.options.readTimeout)
		assert.Equal(t, DefaultWriteTimeout, server.options.writeTimeout)
	})
	t.Run("WithDefaultOptions", func(t *testing.T) {
		t.Parallel()

		healthStore := watcher.NewHealthStore()
		server, err := New(
			nil,
			healthStore,
			WithHost(ServerDefaultHost),
			WithPort(ServerDefaultPort),
			WithReadTimeout(ServerDefaultReadTimeout),
			WithWriteTimeout(ServerDefaultWriteTimeout),
		)
		require.NoError(t, err)
		assert.Equal(t, ServerDefaultHost, server.options.host)
		assert.Equal(t, ServerDefaultPort, server.options.port)
		assert.Equal(t, ServerDefaultReadTimeout, server.options.readTimeout)
		assert.Equal(t, ServerDefaultWriteTimeout, server.options.writeTimeout)
	})
}

func TestStartServer(t *testing.T) {
	t.Parallel()
	healthStore := watcher.NewHealthStore()
	server, err := New(
		nil,
		healthStore,
		WithHost(DefaultHost),
		WithPort(DefaultPort),
		WithReadTimeout(DefaultReadTimeout),
		WithWriteTimeout(DefaultWriteTimeout),
	)
	require.NoError(t, err)

	go func() {
		ticker := time.NewTimer(time.Second * 30)
		<-ticker.C
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	err = server.Start()
	require.ErrorIs(t, err, http.ErrServerClosed)
}
