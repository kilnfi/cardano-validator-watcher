package cardanocli

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type contextKey string

const poolNameCtxKey contextKey = "pool_name"

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
	output, err := cmd.CombinedOutput()

	if cmd.ProcessState != nil {
		if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			maxRSSBytes := usage.Maxrss
			if runtime.GOOS == "linux" {
				maxRSSBytes *= 1024 // Linux reports in KB, convert to bytes
			}
			subcmd := ""
			if len(arg) > 0 {
				subcmd = strings.Join(arg[:min(2, len(arg))], " ")
			}
			attrs := []any{
				slog.String("cmd", name),
				slog.String("subcmd", subcmd),
				slog.Int64("max_rss_mb", maxRSSBytes/1024/1024),
			}
			if poolName, ok := ctx.Value(poolNameCtxKey).(string); ok && poolName != "" {
				attrs = append(attrs, slog.String("pool", poolName))
			}
			slog.DebugContext(ctx, "subprocess peak memory", attrs...)
		}
	}

	return output, err
}
