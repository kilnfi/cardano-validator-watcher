package cardanocli

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	promutils "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// startEchoServer starts a TCP server that echoes back everything it receives
// and returns its RemoteNode address. The server is stopped on test cleanup.
func startEchoServer(t *testing.T) RemoteNode {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	return mustRemoteNode(t, ln.Addr().String())
}

// closedAddr returns a RemoteNode pointing at a port nobody listens on, so a
// dial to it fails fast with connection refused.
func closedAddr(t *testing.T) RemoteNode {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	return mustRemoteNode(t, addr)
}

func mustRemoteNode(t *testing.T, addr string) RemoteNode {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return RemoteNode{Host: host, Port: port}
}

// roundtrip dials the proxy's unix socket, sends payload and returns the echoed
// bytes, proving the proxy reached a working backend.
func roundtrip(t *testing.T, socketPath, payload string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Write([]byte(payload))
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, len(payload))
	n, err := conn.Read(buf)
	require.NoError(t, err)
	return string(buf[:n])
}

func newTestProxy(t *testing.T, nodes []RemoteNode) *SocketProxy {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	socketPath := filepath.Join(t.TempDir(), "proxy.sock")
	proxy, err := NewSocketProxy(ctx, socketPath, nodes, nil)
	require.NoError(t, err)
	proxy.Start(ctx)

	// Give the accept loop a moment to start listening.
	require.Eventually(t, func() bool {
		c, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, time.Second, 10*time.Millisecond)

	return proxy
}

func TestSocketProxy_NoNodes(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "proxy.sock")
	_, err := NewSocketProxy(context.Background(), socketPath, nil, nil)
	require.Error(t, err)
}

func TestSocketProxy_SingleNode(t *testing.T) {
	t.Parallel()

	good := startEchoServer(t)
	proxy := newTestProxy(t, []RemoteNode{good})

	require.Equal(t, "hello", roundtrip(t, proxy.SocketPath(), "hello"))
}

func TestSocketProxy_FailsOverToHealthyNode(t *testing.T) {
	t.Parallel()

	down := closedAddr(t)
	good := startEchoServer(t)

	// Primary is down, secondary is healthy: the proxy must fail over.
	proxy := newTestProxy(t, []RemoteNode{down, good})

	require.Equal(t, "failover", roundtrip(t, proxy.SocketPath(), "failover"))
}

func TestSocketProxy_MarksFailedEndpointDownForCooldown(t *testing.T) {
	t.Parallel()

	down := closedAddr(t)
	good := startEchoServer(t)

	proxy := newTestProxy(t, []RemoteNode{down, good})

	// First connection fails over and marks the primary down.
	require.Equal(t, "one", roundtrip(t, proxy.SocketPath(), "one"))

	require.True(t, proxy.endpoints[0].isDown(time.Now()), "failed primary should be in cooldown")
	require.False(t, proxy.endpoints[1].isDown(time.Now()), "healthy node should not be in cooldown")

	// Subsequent connections keep working while the primary is in cooldown.
	require.Equal(t, "two", roundtrip(t, proxy.SocketPath(), "two"))
}

func TestSocketProxy_PublishesPerNodeMetrics(t *testing.T) {
	t.Parallel()

	down := closedAddr(t)
	good := startEchoServer(t)

	m := metrics.NewCollection()

	ctx := t.Context()

	socketPath := filepath.Join(t.TempDir(), "proxy.sock")
	proxy, err := NewSocketProxy(ctx, socketPath, []RemoteNode{down, good}, m)
	require.NoError(t, err)

	// Probe deterministically rather than waiting for the background ticker.
	proxy.probeAll(ctx)

	// up: the unreachable node is down, the echo server is up.
	require.InDelta(t, 0.0, promutils.ToFloat64(m.CardanoNodeUp.WithLabelValues(down.addr())), 0.0001)
	require.InDelta(t, 1.0, promutils.ToFloat64(m.CardanoNodeUp.WithLabelValues(good.addr())), 0.0001)

	// active: the primary is down, so the healthy node is the active one.
	require.InDelta(t, 0.0, promutils.ToFloat64(m.CardanoNodeActive.WithLabelValues(down.addr())), 0.0001)
	require.InDelta(t, 1.0, promutils.ToFloat64(m.CardanoNodeActive.WithLabelValues(good.addr())), 0.0001)
}
