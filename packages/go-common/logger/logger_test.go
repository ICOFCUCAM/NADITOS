package logger_test

import (
	"log/slog"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/logger"
)

// TestNew_LevelMapping: each documented level string maps to the
// expected slog.Level. Misconfigured services would otherwise log at
// the wrong volume.
func TestNew_LevelMapping(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug}, // case-insensitive
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},        // default
		{"nonsense", slog.LevelInfo}, // fall back, not panic
	}
	for _, tc := range cases {
		l := logger.New(tc.in)
		if l == nil {
			t.Fatalf("%q: nil logger", tc.in)
		}
		// We can't read the handler's level directly via the public
		// API, but we can probe via Enabled.
		got := slog.LevelDebug
		switch {
		case l.Enabled(nil, slog.LevelDebug):
			got = slog.LevelDebug
		case l.Enabled(nil, slog.LevelInfo):
			got = slog.LevelInfo
		case l.Enabled(nil, slog.LevelWarn):
			got = slog.LevelWarn
		case l.Enabled(nil, slog.LevelError):
			got = slog.LevelError
		}
		if got != tc.want {
			t.Errorf("%q: want %v, got %v", tc.in, tc.want, got)
		}
	}
}
