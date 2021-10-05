package main

import (
	"context"
	"flag"

	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	var loglvl string
	flag.StringVar(&loglvl, "loglevel", "debug", "Log level")
	flag.Parse()

	eng, err := engine.New(loglvl)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create new engine")
	}
	eng.Logger.Info().Msg("kube-ns-suspender launched")

	// create the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create in-cluster configuration")
	}
	eng.Logger.Debug().Msg("in-cluster configuration successfully created")

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create the clientset")
	}
	eng.Logger.Debug().Msg("clientset successfully created")

	ctx := context.Background()
	go eng.Watcher(ctx, clientset)
	go eng.Suspender(ctx, clientset)

	// wait forever
	select {}
}
