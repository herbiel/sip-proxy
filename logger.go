package main

import (
	"log/slog"
	"os"
	"strings"
)

var logger *slog.Logger

func init() {
	// Get log format from environment variable (default: text)
	// Set LOG_FORMAT=text for human-readable text output
	// Set LOG_FORMAT=json for structured JSON output
	logFormat := strings.ToLower(os.Getenv("LOG_FORMAT"))
	if logFormat == "" {
		logFormat = "text"
	}

	// Get log level from environment variable (default: debug)
	// Options: debug, info, warn, error
	logLevel := strings.ToLower(os.Getenv("LOG_LEVEL"))
	var level slog.Level
	switch logLevel {
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelDebug
	}

	options := &slog.HandlerOptions{
		Level: level,
	}

	// Choose handler based on format
	var handler slog.Handler
	if logFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}

	logger = slog.New(handler)

	// Set as default logger
	slog.SetDefault(logger)

	// Log the configuration
	slog.Info("Logger initialized",
		"format", logFormat,
		"level", level.String())
}
