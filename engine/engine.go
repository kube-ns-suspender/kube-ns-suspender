package engine

import (
	"os"
	"sync"
	"time"

	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/rs/zerolog"
	v1 "k8s.io/api/core/v1"
)

const (
	running   = "Running"
	suspended = "Suspended"
	forced    = "RunningForced"
)

type Engine struct {
	Logger               zerolog.Logger
	Mutex                sync.Mutex
	Wl                   chan v1.Namespace
	MetricsServ          metrics.Server
	RunningForcedHistory map[string]time.Time
	Options              Options
}

type Options struct {
	WatcherIdle                   int
	LogLevel, TZ                  string
	DryRun, NoKubeWarnings, WebUI bool
}

// New returns a new engine instance
func New(opt Options) (*Engine, error) {
	var err error
	e := Engine{
		Logger:  zerolog.New(os.Stderr).With().Timestamp().Logger(),
		Wl:      make(chan v1.Namespace, 50),
		Options: opt,
	}

	lvl, err := zerolog.ParseLevel(e.Options.LogLevel)
	if err != nil {
		return nil, err
	}
	e.Logger = e.Logger.Level(lvl)
	return &e, nil
}

func flip(i int32) *int32 {
	return &i
}

func getTimes(suspendAt string) (int, int, error) {
	suspendTime, err := time.Parse(time.Kitchen, suspendAt)
	if err != nil {
		return 0, 0, err
	}
	suspendTimeInt := suspendTime.Minute() + suspendTime.Hour()*60

	now := time.Now().Local()
	nowInt := now.Minute() + now.Hour()*60
	return nowInt, suspendTimeInt, nil
}
