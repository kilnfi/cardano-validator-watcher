package watcher

import "errors"

var (
	ErrBlockFrostAPINotReachable = errors.New("blockfrost API is not reachable")
	ErrCardanoNodeNotReachable   = errors.New("cardano node is not reachable")
)
