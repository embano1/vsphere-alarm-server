package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
)

var (
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

	if err := run(ctx, os.Args, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatalf("error running server: %v", err)
	}
}

func run(ctx context.Context, _ []string, _ io.Writer) error {
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

	// fmt.Printf("DEBUG: logging out from vc: %v", vc.Logout(ctx))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return srv.ceClient.StartReceiver(egCtx, srv.handleEvent)
	})

	eg.Go(func() error {
		<-egCtx.Done()
		_ = srv.vcClient.Logout(context.TODO())
		return nil
	})

	eg.Go(func() error {
		return srv.cache.run(egCtx)
	})

	eg.Go(func() error {
		select {
		case <-egCtx.Done():
			return nil
		case err = <-srv.errCh:
			return err
		}
	})

	return eg.Wait()
}
