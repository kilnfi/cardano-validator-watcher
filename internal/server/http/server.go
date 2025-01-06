package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	ServerDefaultReadTimeout  time.Duration = 15 * time.Second
	ServerDefaultWriteTimeout time.Duration = 30 * time.Second
	ServerDefaultHost         string        = "127.0.0.0"
	ServerDefaultPort         int           = 8080
)

type Server struct {
	logger *slog.Logger
	router *http.ServeMux
	server *http.Server

	registry *prometheus.Registry

	options *options
}

func New(
	registry *prometheus.Registry,
	opts ...ServerOptionsFunc,
) (*Server, error) {
	logger := slog.With(
		slog.String("component", "http-server"),
	)

	options := &options{
		host:         ServerDefaultHost,
		port:         ServerDefaultPort,
		readTimeout:  ServerDefaultReadTimeout,
		writeTimeout: ServerDefaultWriteTimeout,
	}

	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	router := http.NewServeMux()
	addr := fmt.Sprintf("%s:%d", options.host, options.port)

	server := &Server{
		logger: logger,
		router: router,
		server: &http.Server{
			Addr:         addr,
			Handler:      router,
			ReadTimeout:  options.readTimeout,
			WriteTimeout: options.writeTimeout,
		},
		registry: registry,
		options:  options,
	}

	server.registerRoutes()

	return server, nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	if err := s.server.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

// Stop stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
}

func (s *Server) registerRoutes() {
	handler := NewHandler(s.logger)

	s.router.HandleFunc("GET /", handler.Default)
	s.router.HandleFunc("GET /livez", handler.LiveProbe)
	s.router.HandleFunc("GET /readyz", handler.ReadyProbe)
	s.router.Handle("GET /metrics", promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))
}
