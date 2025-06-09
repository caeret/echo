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
	With(args ...any) Logger
}

type LoggerWriter struct {
	Logger Logger
}

func (w *LoggerWriter) Write(b []byte) (int, error) {
	if w.Logger != nil {
		w.Logger.Error(string(b))
	}
	return len(b), nil
}

func (e *Echo) SetLogger(logger Logger) {
	e.Logger = logger
	e.StdLogger = log.New(&LoggerWriter{logger}, "echo: ", 0)
}

type SlogLogger struct {
	Logger *slog.Logger
}

func (l *SlogLogger) Debug(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Debug(msg, args...)
		return
	}
	slog.Debug(msg, args...)
}

func (l *SlogLogger) Info(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Info(msg, args...)
		return
	}
	slog.Info(msg, args...)
}

func (l *SlogLogger) Warn(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Warn(msg, args...)
		return
	}
	slog.Warn(msg, args...)
}

func (l *SlogLogger) Error(msg string, args ...any) {
	if l.Logger != nil {
		l.Logger.Error(msg, args...)
		return
	}
	slog.Error(msg, args...)
}

func (l *SlogLogger) With(args ...any) Logger {
	if l.Logger != nil {
		return &SlogLogger{Logger: l.Logger.With(args...)}
	}
	return &SlogLogger{slog.With(args...)}
}
