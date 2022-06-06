package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxKey struct{}

var defaultLogger *zap.Logger

func init() {
	if l, err := zap.NewProduction(); err != nil {
		panic("create logger: " + err.Error())
	} else {
		defaultLogger = l.Named("vsphere")
	}
}

// Set stores the provided logger in a child context of ctx and returns the child context
func Set(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// Get returns the logger stored in the provided context. Returns a default zap
// production logger if none or an invalid logger is stored in context.
func Get(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*zap.Logger); ok {
		return l
	}
	return defaultLogger
}
