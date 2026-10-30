package main

import (
	"github.com/gotd/log"
	"github.com/gotd/log/logslog"
	"log/slog"
	"os"
)

func diagnosticLogger() log.Logger {
	if os.Getenv("MTPROTO_DEBUG") != "true" {
		return log.Nop
	}
	return logslog.New(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}
