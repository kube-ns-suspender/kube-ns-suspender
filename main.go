package main

import (
	"context"
	"os"
	"time"

	"github.com/namsral/flag"

	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	var opt engine.Options
	var err error
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], "KUBE_NS_SUSPENDER", 0)
	fs.StringVar(&opt.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&opt.TZ, "timezone", "Europe/Paris", "Timezone to use")
	fs.StringVar(&opt.Prefix, "prefix", "kube-ns-suspender/", "Prefix to use for annotations")
	fs.IntVar(&opt.WatcherIdle, "watcher-idle", 15, "Watcher idle duration (in seconds)")
	fs.IntVar(&opt.RunningDuration, "running-duration", 4, "Running duration (in hours)")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "Run in dry run mode")
	fs.BoolVar(&opt.NoKubeWarnings, "no-kube-warnings", false, "Disable Kubernetes warnings")
	fs.BoolVar(&opt.HumanLogs, "human", false, "Disable JSON logging")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal().Err(err).Msg("cannot parse flags")
	}

	// set the local timezone
	time.Local, err = time.LoadLocation(opt.TZ)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot load timezone")
	}

	// create the engine
	start := time.Now()
	eng, err := engine.New(opt)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create new engine")
	}
	eng.Logger.Info().Msgf("engine successfully created in %s", time.Since(start))

	eng.Logger.Debug().Msgf("timezone: %s", time.Local.String())
	eng.Logger.Debug().Msgf("watcher idle: %s", time.Duration(eng.Options.WatcherIdle)*time.Second)
	eng.Logger.Debug().Msgf("running duration: %s", time.Duration(eng.Options.RunningDuration)*time.Hour)
	eng.Logger.Debug().Msgf("log level: %s", eng.Options.LogLevel)
	eng.Logger.Debug().Msgf("json logging: %v", !eng.Options.HumanLogs)

	// create metrics server
	start = time.Now()
	eng.MetricsServ = *metrics.Init()
	// start metrics server
	go func() {
		if err := eng.MetricsServ.Start(); err != nil {
			eng.Logger.Fatal().Err(err).Msg("metrics server failed")
		}
	}()
	eng.Logger.Info().Msgf("metrics server successfully created in %s", time.Since(start))

	// create the in-cluster config
	start = time.Now()
	config, err := rest.InClusterConfig()
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create in-cluster configuration")
	}
	eng.Logger.Info().Msgf("in-cluster configuration successfully created in %s", time.Since(start))

	// disable k8s warnings
	if eng.Options.NoKubeWarnings {
		config.WarningHandler = rest.NoWarnings{}
	}

	// create the clientset
	start = time.Now()
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create the clientset")
	}
	eng.Logger.Info().Msgf("clientset successfully created in %s", time.Since(start))

	ctx := context.Background()
	go eng.Watcher(ctx, clientset)
	go eng.Suspender(ctx, clientset)

	// wait forever
	select {}
}
