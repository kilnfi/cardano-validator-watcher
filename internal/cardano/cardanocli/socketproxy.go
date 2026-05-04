package cardanocli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"
)

// SocketProxy creates a Unix domain socket and proxies connections to a remote TCP endpoint.
type SocketProxy struct {
	socketPath string
	remoteAddr string
	listener   net.Listener
	logger     *slog.Logger
}

func NewSocketProxy(ctx context.Context, socketPath, remoteHost string, remotePort int) (*SocketProxy, error) {
	_ = os.Remove(socketPath)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on unix socket %s: %w", socketPath, err)
	}

	logger := slog.With(slog.String("component", "socket-proxy"))
	logger.Info("unix socket proxy ready",
		slog.String("socket", socketPath),
		slog.String("remote", fmt.Sprintf("%s:%d", remoteHost, remotePort)),
	)

	return &SocketProxy{
		socketPath: socketPath,
		remoteAddr: fmt.Sprintf("%s:%d", remoteHost, remotePort),
		listener:   listener,
		logger:     logger,
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

func (p *SocketProxy) proxy(ctx context.Context, local net.Conn) {
	defer local.Close()

	remote, err := (&net.Dialer{}).DialContext(ctx, "tcp", p.remoteAddr)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to connect to remote",
			slog.String("remote", p.remoteAddr),
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
