package http

import "time"

type options struct {
	host         string
	port         int
	readTimeout  time.Duration
	writeTimeout time.Duration
}

type ServerOptionsFunc func(*options) error

func WithHost(host string) ServerOptionsFunc {
	return func(o *options) error {
		o.host = host
		return nil
	}
}

func WithPort(port int) ServerOptionsFunc {
	return func(o *options) error {
		o.port = port
		return nil
	}
}

func WithReadTimeout(timeout time.Duration) ServerOptionsFunc {
	return func(o *options) error {
		o.readTimeout = timeout
		return nil
	}
}

func WithWriteTimeout(timeout time.Duration) ServerOptionsFunc {
	return func(o *options) error {
		o.writeTimeout = timeout
		return nil
	}
}
