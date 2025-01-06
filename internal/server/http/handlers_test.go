package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/watcher"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultHandler(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_DefaultHandlerIsOk", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		healthStore := watcher.NewHealthStore()
		server, err := New(
			nil,
			healthStore,
		)

		require.NoError(t, err)
		server.router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
	})

	t.Run("GoodPath_DefaultHandlerShouldReturn404ForUnknownPath", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/fake", nil)
		w := httptest.NewRecorder()

		healthStore := watcher.NewHealthStore()
		server, err := New(
			nil,
			healthStore,
		)

		require.NoError(t, err)
		server.router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestLiveProbe(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_LiveProbeIsReady", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/livez", nil)
		w := httptest.NewRecorder()

		healthStore := watcher.NewHealthStore()
		server, err := New(
			nil,
			healthStore,
		)
		require.NoError(t, err)
		server.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestReadyProbe(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_ReadyProbeIsReady", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		healthStore := watcher.NewHealthStore()
		healthStore.SetHealth(true)
		server, err := New(
			nil,
			healthStore,
		)
		require.NoError(t, err)
		server.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("SadPath_ReadyProbeIsNotReady", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		healthStore := watcher.NewHealthStore()
		healthStore.SetHealth(false)
		server, err := New(
			nil,
			healthStore,
		)
		require.NoError(t, err)
		server.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestMetricsHandler(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_MetricsHandlerIsOk", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()

		registry := prometheus.NewRegistry()
		metrics := metrics.NewCollection()
		metrics.MustRegister(registry)

		healthStore := watcher.NewHealthStore()
		server, err := New(
			registry,
			healthStore,
		)

		require.NoError(t, err)
		server.router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
