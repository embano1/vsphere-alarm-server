package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
)

var (
	// GitSHA is set during CI as the short Git commit revision for a particular
	// build
	GitSHA = "unknown"
)

func main() {
	printVersion := flag.Bool("v", false, "print version information")
	flag.Parse()

	if *printVersion {
		fmt.Printf("commit: %s\n", GitSHA)
		os.Exit(0)
	}

	var env envConfig
	if err := envconfig.Process(envPrefix, &env); err != nil {
		panic(fmt.Errorf("process env var: %w", err).Error())
	}

	ctx := signals.NewContext()
	var logger *zap.SugaredLogger

	if env.Debug {
		if logDev, err := zap.NewDevelopment(); err != nil {
			panic(fmt.Errorf("create logger: %w", err).Error())
		} else {
			logger = logDev.Sugar().Named("vsphere-alarm-server").With("commit", GitSHA)
		}
	} else {
		if logProd, err := zap.NewProduction(zap.AddStacktrace(zap.ErrorLevel)); err != nil {
			panic(fmt.Errorf("create logger: %w", err).Error())
		} else {
			logger = logProd.Sugar().Named("vsphere-alarm-server").With("commit", GitSHA)
		}
	}

	ctx = logging.WithLogger(ctx, logger)

	if err := run(ctx, os.Args); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatalf("error running server: %v", err)
	}
}

func run(ctx context.Context, _ []string) error {
	var env envConfig
	if err := envconfig.Process(envPrefix, &env); err != nil {
		return fmt.Errorf("process env var: %w", err)
	}

	srv, err := newAlarmServer(ctx)
	if err != nil {
		return err
	}

	logger := logging.FromContext(ctx)
	logger.Infow("starting vsphere alarm server", "port", env.Port, "cache_ttl", srv.cache.ttl, "debug", env.Debug, "event_suffix", env.EventSuffix, "alarm_info_key", env.InjectKey)

	return srv.run(ctx)
}
