package cardanocli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
)

const (
	dialTimeout = 10 * time.Second
	// failoverCooldown is how long a remote endpoint is skipped after a failed
	// dial. It prevents paying the dial timeout repeatedly on a node that is
	// down while other endpoints are available, and lets the proxy fail back to
	// a higher-priority node automatically once the cooldown expires.
	failoverCooldown = 30 * time.Second
	// healthProbeInterval is how often the proxy actively probes every endpoint
	// (including standby ones) so the up/active metrics reflect the real state
	// of each node even when it is not currently serving traffic.
	healthProbeInterval = 15 * time.Second
	// probeTimeout bounds a single health-probe dial.
	probeTimeout = 5 * time.Second
)

// RemoteNode identifies a cardano-node TCP endpoint, typically a socat bridge
// exposing the node's Unix socket over TCP.
type RemoteNode struct {
	Host string
	Port int
}

func (n RemoteNode) addr() string {
	return fmt.Sprintf("%s:%d", n.Host, n.Port)
}

// remoteEndpoint tracks the availability of a single remote so failed nodes can
// be skipped for a cooldown window.
type remoteEndpoint struct {
	addr string

	mu        sync.Mutex
	downUntil time.Time
}

func (e *remoteEndpoint) isDown(now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return now.Before(e.downUntil)
}

func (e *remoteEndpoint) markDown(until time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.downUntil = until
}

func (e *remoteEndpoint) clearDown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.downUntil = time.Time{}
}

// SocketProxy creates a Unix domain socket and proxies connections to one or
// more remote TCP endpoints, failing over between them when a node is down.
type SocketProxy struct {
	socketPath string
	endpoints  []*remoteEndpoint
	listener   net.Listener
	logger     *slog.Logger
	metrics    *metrics.Collection
}

// NewSocketProxy creates the proxy. metrics may be nil, in which case no
// per-node metrics are emitted.
func NewSocketProxy(ctx context.Context, socketPath string, nodes []RemoteNode, m *metrics.Collection) (*SocketProxy, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("socket proxy requires at least one remote node")
	}

	_ = os.Remove(socketPath)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on unix socket %s: %w", socketPath, err)
	}

	endpoints := make([]*remoteEndpoint, 0, len(nodes))
	addrs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		endpoints = append(endpoints, &remoteEndpoint{addr: n.addr()})
		addrs = append(addrs, n.addr())
	}

	logger := slog.With(slog.String("component", "socket-proxy"))
	logger.Info("unix socket proxy ready",
		slog.String("socket", socketPath),
		slog.Any("remotes", addrs),
	)

	return &SocketProxy{
		socketPath: socketPath,
		endpoints:  endpoints,
		listener:   listener,
		logger:     logger,
		metrics:    m,
	}, nil
}

func (p *SocketProxy) SocketPath() string {
	return p.socketPath
}

func (p *SocketProxy) Start(ctx context.Context) {
	go func() {
		<-ctx.Done()
		p.listener.Close()
		os.Remove(p.socketPath)
	}()

	go p.runHealthProbe(ctx)

	go func() {
		for {
			conn, err := p.listener.Accept()
			if err != nil {
				// listener was closed — normal shutdown
				return
			}
			go p.proxy(ctx, conn)
		}
	}()
}

// runHealthProbe periodically dials every endpoint so the per-node metrics
// reflect the true state of each node, including standby ones that are not
// currently serving traffic (otherwise we would only learn a standby is down at
// the moment we need to fail over to it).
func (p *SocketProxy) runHealthProbe(ctx context.Context) {
	ticker := time.NewTicker(healthProbeInterval)
	defer ticker.Stop()

	for {
		p.probeAll(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// probeAll opens and immediately closes a TCP connection to every endpoint to
// check reachability, updating each endpoint's availability and the metrics.
func (p *SocketProxy) probeAll(ctx context.Context) {
	for _, ep := range p.endpoints {
		dialCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", ep.addr)
		cancel()
		if err != nil {
			ep.markDown(time.Now().Add(failoverCooldown))
			continue
		}
		_ = conn.Close()
		ep.clearDown()
	}
	p.updateMetrics()
}

// updateMetrics publishes the up/active gauge for each endpoint. The active
// endpoint is the highest-priority one that is currently reachable, matching
// what dialRemote would pick.
func (p *SocketProxy) updateMetrics() {
	if p.metrics == nil {
		return
	}

	now := time.Now()
	activeSet := false
	for _, ep := range p.endpoints {
		up := !ep.isDown(now)
		if up {
			p.metrics.CardanoNodeUp.WithLabelValues(ep.addr).Set(1)
		} else {
			p.metrics.CardanoNodeUp.WithLabelValues(ep.addr).Set(0)
		}

		active := 0.0
		if up && !activeSet {
			active = 1
			activeSet = true
		}
		p.metrics.CardanoNodeActive.WithLabelValues(ep.addr).Set(active)
	}
}

// dialRemote connects to the first reachable remote, trying them in configured
// priority order. Endpoints that recently failed are skipped for
// failoverCooldown so we don't repeatedly pay the dial timeout on a down node;
// if every endpoint is in cooldown they are all retried as a last resort.
func (p *SocketProxy) dialRemote(ctx context.Context) (net.Conn, error) {
	defer p.updateMetrics()

	now := time.Now()

	candidates := make([]*remoteEndpoint, 0, len(p.endpoints))
	for _, ep := range p.endpoints {
		if !ep.isDown(now) {
			candidates = append(candidates, ep)
		}
	}
	if len(candidates) == 0 {
		candidates = p.endpoints
	}

	var lastErr error
	for _, ep := range candidates {
		remote, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", ep.addr)
		if err != nil {
			ep.markDown(time.Now().Add(failoverCooldown))
			lastErr = err
			p.logger.WarnContext(ctx, "failed to connect to remote, trying next endpoint",
				slog.String("remote", ep.addr),
				slog.String("error", err.Error()),
			)
			continue
		}
		ep.clearDown()
		return remote, nil
	}

	return nil, lastErr
}

func (p *SocketProxy) proxy(ctx context.Context, local net.Conn) {
	defer local.Close()

	remote, err := p.dialRemote(ctx)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to connect to any remote",
			slog.String("error", err.Error()),
		)
		return
	}
	defer remote.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Force-close both connections when context is done (app shutdown OR
	// one direction finished), so any blocked io.Copy returns immediately.
	go func() {
		<-ctx.Done()
		now := time.Now()
		local.SetDeadline(now)  //nolint:errcheck
		remote.SetDeadline(now) //nolint:errcheck
	}()

	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, local); done <- struct{}{} }() //nolint:errcheck
	go func() { io.Copy(local, remote); done <- struct{}{} }() //nolint:errcheck

	<-done
	// One direction finished — cancel the context to trigger the watcher
	// goroutine above, which force-closes both connections. Without this,
	// io.Copy(local, remote) stays blocked on remote.Read() indefinitely
	// when cardano-cli is killed, leaking a socat child process per call.
	cancel()
	<-done
}
