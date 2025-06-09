// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"log"
	"log/slog"
)

// Logger defines the logging interface.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// LoggerWriter implements the io.Writer interface.
type LoggerWriter struct {
	Logger Logger
}

// Write writes the contents of the byte slice b to the logger.
func (w *LoggerWriter) Write(b []byte) (int, error) {
	if w.Logger != nil {
		w.Logger.Error(string(b))
	}
	return len(b), nil
}

// SetLogger sets the logger
func (e *Echo) SetLogger(logger Logger) {
	e.Logger = logger
	e.StdLogger = log.New(&LoggerWriter{logger}, "echo: ", 0)
}

// SlogLogger is an implementation of the Logger interface based on
type SlogLogger struct {
	Logger *slog.Logger
}

// Debug logs a debug-level message. If a custom logger is set, it uses
// that logger to log the message; otherwise, it falls back to the
// default slog package. The message and any additional arguments are
// passed to the logger.
func (l *SlogLogger) Debug(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Debug(msg, args...)
		return
	}
	slog.Debug(msg, args...)
}

// Info logs an info-level message. If a custom logger is set, it uses
// that logger to log the message; otherwise, it falls back to the
// default slog package. The message and any additional arguments are
// passed to the logger.
func (l *SlogLogger) Info(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Info(msg, args...)
		return
	}
	slog.Info(msg, args...)
}

// Warn logs a warning-level message. If a custom logger is set, it uses
// that logger to log the message; otherwise, it falls back to the
// default slog package. The message and any additional arguments are
// passed to the logger.
func (l *SlogLogger) Warn(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Warn(msg, args...)
		return
	}
	slog.Warn(msg, args...)
}

// Error logs an error-level message. If a custom logger is set, it uses
// that logger to log the message; otherwise, it falls back to the
// default slog package. The message and any additional arguments are
// passed to the logger.
func (l *SlogLogger) Error(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Error(msg, args...)
		return
	}
	slog.Error(msg, args...)
}
