package main

import (
	"context"
	"os"
	"time"

	"github.com/namsral/flag"

	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/govirtuo/kube-ns-suspender/pprof"
	"github.com/govirtuo/kube-ns-suspender/webui"
	"github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

var (
	// Version holds the build version
	Version string
	// BuildDate holds the build date
	BuildDate string
)

func main() {
	var opt engine.Options
	var err error

	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], "KUBE_NS_SUSPENDER", 0)
	fs.StringVar(&opt.LogLevel, "log-level", "debug", "Log level")
	fs.StringVar(&opt.TZ, "timezone", "Europe/Paris", "Timezone to use")
	fs.StringVar(&opt.Prefix, "prefix", "kube-ns-suspender/", "Prefix to use for annotations")
	fs.StringVar(&opt.ControllerName, "controller-name", "kube-ns-suspender", "Unique name of the controller")
	fs.StringVar(&opt.RunningDuration, "running-duration", "4h", "Running duration")
	fs.IntVar(&opt.WatcherIdle, "watcher-idle", 15, "Watcher idle duration (in seconds)")
	fs.BoolVar(&opt.NoKubeWarnings, "no-kube-warnings", false, "Disable Kubernetes warnings")
	fs.BoolVar(&opt.HumanLogs, "human", false, "Disable JSON logging")
	fs.BoolVar(&opt.EmbeddedUI, "ui-embedded", false, "Start UI in background")
	fs.BoolVar(&opt.WebUIOnly, "ui-only", false, "Start UI only")
	fs.BoolVar(&opt.PProf, "pprof", false, "Start pprof server")
	fs.StringVar(&opt.PProfAddr, "pprof-addr", ":4455", "Address and port to use with pprof")
	fs.StringVar(&opt.SlackChannelName, "slack-channel-name", "", "Name of the help Slack channel in the UI bug page")
	fs.StringVar(&opt.SlackChannelLink, "slack-channel-link", "", "Link of the helm Slack channel in the UI bug page")
	fs.BoolVar(&opt.KedaEnabled, "keda-enabled", false, "Enable pausing of Keda.sh scaledobjects")
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
	eng.Logger.Info().Msgf("kube-ns-suspender version '%s' (built %s)", Version, BuildDate)

	if eng.Options.PProf {
		s, err := pprof.New(eng.Options.PProfAddr)
		if err != nil {
			eng.Logger.Fatal().Err(err).Msg("cannot start pprof")
		}
		eng.Logger.Info().Msgf("starting pprof on %s", eng.Options.PProfAddr)
		go s.Run()
	}

	// start web ui
	if eng.Options.EmbeddedUI || eng.Options.WebUIOnly {
		go func() {
			uiLogger := eng.Logger.With().Str("routine", "webui").Logger()
			if err := webui.Start(uiLogger, "8080",
				eng.Options.Prefix, eng.Options.ControllerName, Version, BuildDate, opt.SlackChannelName, opt.SlackChannelLink); err != nil {
				uiLogger.Fatal().Err(err).Msg("web UI failed")
			}
		}()
		eng.Logger.Info().Msg("web UI successfully created")
		if eng.Options.WebUIOnly {
			eng.Logger.Info().Msg("starting web UI only")
			// if we want only the webui, we have to wait here forever after the creation
			select {}
		}
	}

	eng.Logger.Debug().Msgf("timezone: %s", time.Local.String())
	eng.Logger.Debug().Msgf("watcher idle: %s", time.Duration(eng.Options.WatcherIdle)*time.Second)
	eng.Logger.Debug().Msgf("running duration: %s", eng.RunningDuration)
	eng.Logger.Debug().Msgf("log level: %s", eng.Options.LogLevel)
	eng.Logger.Debug().Msgf("json logging: %v", !eng.Options.HumanLogs)
	eng.Logger.Debug().Msgf("controller name: %v", eng.Options.ControllerName)
	eng.Logger.Debug().Msgf("annotations prefix: %v", eng.Options.Prefix)

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
		eng.Logger.Info().Msgf("Kubernetes warnings disabled")
	}

	// create the clientset
	start = time.Now()
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create the clientset")
	}
	eng.Logger.Info().Msgf("clientset successfully created in %s", time.Since(start))

	// create the keda client
	kedaclient := &v1alpha1.KedaV1alpha1Client{}
	if eng.Options.KedaEnabled {
		start = time.Now()
		kedaclient, err = v1alpha1.NewForConfig(config)
		if err != nil {
			eng.Logger.Fatal().Err(err).Msg("cannot create the keda client")
		}
		eng.Logger.Info().Msgf("keda client successfully created in %s", time.Since(start))
	} else {
		eng.Logger.Info().Msg("keda is disabled")
	}

	eng.Logger.Info().Msgf("starting 'Watcher' and 'Suspender' routines")
	ctx := context.Background()
	go eng.Watcher(ctx, clientset)
	go eng.Suspender(ctx, clientset, kedaclient)

	// wait forever
	select {}
}
