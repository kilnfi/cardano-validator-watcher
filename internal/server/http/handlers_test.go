package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
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

		server, err := New(
			nil,
		)

		require.NoError(t, err)
		server.router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusMovedPermanently, w.Code)
	})

	t.Run("GoodPath_DefaultHandlerShouldReturn404ForUnknownPath", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequest(http.MethodGet, "/fake", nil)
		w := httptest.NewRecorder()

		server, err := New(
			nil,
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

		server, err := New(
			nil,
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

		server, err := New(
			nil,
		)
		require.NoError(t, err)
		server.router.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
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

		server, err := New(
			registry,
		)

		require.NoError(t, err)
		server.router.ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
