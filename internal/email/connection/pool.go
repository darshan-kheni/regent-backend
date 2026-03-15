package connection

import (
	"log/slog"
	"os"
	"syscall"
)

// CheckFDLimits validates file descriptor limits on startup.
// Fatal if < 2048, warn if < 4096.
func CheckFDLimits() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		slog.Warn("could not check file descriptor limit", "error", err)
		return
	}
	slog.Info("file descriptor limits", "soft", rLimit.Cur, "hard", rLimit.Max)
	if rLimit.Cur < 2048 {
		slog.Error("file descriptor limit too low for IMAP connections",
			"current", rLimit.Cur, "minimum", 2048)
		os.Exit(1)
	}
	if rLimit.Cur < 4096 {
		slog.Warn("file descriptor limit is low, recommend >= 4096",
			"current", rLimit.Cur)
	}
}
