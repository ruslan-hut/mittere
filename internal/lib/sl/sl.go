package sl

import (
	"log/slog"
)

func Err(err error) slog.Attr {
	return slog.Attr{
		Key:   "error",
		Value: slog.StringValue(err.Error()),
	}
}

// Secret redacts sensitive values in logs
func Secret(key, value string) slog.Attr {
	r := "[REDACTED]"
	if value == "" {
		r = "[EMPTY]"
	}
	return slog.Attr{
		Key:   key,
		Value: slog.StringValue(r),
	}
}

func Module(mod string) slog.Attr {
	return slog.Attr{
		Key:   "mod",
		Value: slog.StringValue(mod),
	}
}
