package main

import (
	"context"
	"flag"

	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	var loglvl, tz string
	flag.StringVar(&loglvl, "loglevel", "debug", "Log level")
	flag.StringVar(&tz, "timezone", "Europe/Paris", "Timezone to use")
	flag.Parse()

	// create the engine
	eng, err := engine.New(loglvl, tz)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create new engine")
	}
	eng.Logger.Info().Msg("kube-ns-suspender launched")

	// create metrics server
	eng.MetricsServ = *metrics.Init()

	// start metrics server
	go func() {
		if err := eng.MetricsServ.Start(); err != nil {
			eng.Logger.Fatal().Err(err).Msg("metrics server failed")
		}
	}()
	eng.Logger.Info().Msg("metrics server successfully created")

	// create the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create in-cluster configuration")
	}
	eng.Logger.Info().Msg("in-cluster configuration successfully created")

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create the clientset")
	}
	eng.Logger.Info().Msg("clientset successfully created")

	ctx := context.Background()
	go eng.Watcher(ctx, clientset)
	go eng.Suspender(ctx, clientset)

	// wait forever
	select {}
}
