package api

import (
	"fmt"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func newStructuredLogger(logger zerolog.Logger) func(next http.Handler) http.Handler {
	return chimiddleware.RequestLogger(&structuredLogger{logger})
}

type structuredLogger struct {
	Logger zerolog.Logger
}

func (l *structuredLogger) NewLogEntry(r *http.Request) chimiddleware.LogEntry {
	entry := &structuredLoggerEntry{Logger: l.Logger}
	apiLogger := log.With().
		Str("component", "api").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote_addr", r.RemoteAddr).
		Str("referer", r.Referer()).
		Logger()

	if reqID := getRequestID(r.Context()); reqID != "" {
		apiLogger = apiLogger.With().Str("request_id", reqID).Logger()
	}

	entry.Logger = apiLogger
	entry.Logger.Info().Msg("request started")
	return entry
}

type structuredLoggerEntry struct {
	Logger zerolog.Logger
}

func (l *structuredLoggerEntry) Write(status, bytes int, _ http.Header, elapsed time.Duration, _ any) {
	l.Logger = l.Logger.With().
		Int("status", status).
		Int64("duration", elapsed.Nanoseconds()).
		Logger()
	l.Logger.Info().Msg("request completed")
}

func (l *structuredLoggerEntry) Panic(v interface{}, stack []byte) {
	panicLogger := l.Logger.With().
		Str("stack", string(stack)).
		Str("panic", fmt.Sprintf("%+v", v)).
		Logger()
	panicLogger.Panic().Msg("unhandled request panic")
}

func getLogEntry(r *http.Request) *zerolog.Logger {
	entry, _ := chimiddleware.GetLogEntry(r).(*structuredLoggerEntry)
	if entry == nil {
		return &log.Logger
	}
	return &entry.Logger
}

func logEntrySetField(r *http.Request, key string, value interface{}) *zerolog.Logger {
	if entry, ok := r.Context().Value(chimiddleware.LogEntryCtxKey).(*structuredLoggerEntry); ok {
		entry.Logger = entry.Logger.With().Interface(key, value).Logger()
		return &entry.Logger
	}
	return nil
}
