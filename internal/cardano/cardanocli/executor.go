package cardanocli

import (
	"context"
	"os/exec"
)

type CommandExecutor interface {
	ExecCommand(ctx context.Context, envs []string, name string, arg ...string) ([]byte, error)
}

type RealCommandExecutor struct{}

var _ CommandExecutor = (*RealCommandExecutor)(nil)

//nolint:wrapcheck
func (r *RealCommandExecutor) ExecCommand(ctx context.Context, envs []string, name string, arg ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = append(cmd.Environ(), envs...)
	return cmd.CombinedOutput()
}
