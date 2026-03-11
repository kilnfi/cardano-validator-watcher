package cardanocli

import (
	"context"
	"os/exec"
	"time"
)

type CommandExecutor interface {
	ExecCommand(ctx context.Context, timeout time.Duration, envs []string, name string, arg ...string) ([]byte, error)
}

type RealCommandExecutor struct{}

var _ CommandExecutor = (*RealCommandExecutor)(nil)

//nolint:wrapcheck
func (r *RealCommandExecutor) ExecCommand(ctx context.Context, timeout time.Duration, envs []string, name string, arg ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = append(cmd.Environ(), envs...)
	return cmd.CombinedOutput()
}
