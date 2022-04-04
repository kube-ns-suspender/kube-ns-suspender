package engine

import (
	"os"
	"sync"
	"time"

	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/rs/zerolog"
	v1 "k8s.io/api/core/v1"
)

// those states constants they are used with the annotation desiredState.
// they need to be exported as they are used across packages (in webui...)
const (
	Running   = "Running"
	Suspended = "Suspended"
)

// annotations constants
const (
	// annotations used on namespaces
	nextSuspendTime = "nextSuspendTime"
	controllerName  = "controllerName"

	// those ones need to be exported as they are used
	// in the webui package
	DailySuspendTime = "dailySuspendTime"
	DesiredState     = "desiredState"

	// annotation used on resources (deployments, statefulsets...)
	originalReplicas = "originalReplicas"
)

type Engine struct {
	Logger                zerolog.Logger
	Mutex                 sync.Mutex
	Wl                    chan v1.Namespace
	MetricsServ           metrics.Server
	RunningNamespacesList map[string]time.Time
	RunningDuration       time.Duration
	Options               Options
}

type Options struct {
	WatcherIdle               int
	RunningDuration           string
	LogLevel                  string
	TZ                        string
	Prefix, ControllerName    string
	NoKubeWarnings, HumanLogs bool
	EmbeddedUI, WebUIOnly     bool
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
	if e.Options.HumanLogs {
		e.Logger = e.Logger.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	if e.Options.Prefix[len(e.Options.Prefix)-1] != '/' {
		e.Options.Prefix = e.Options.Prefix + "/"
	}

	e.RunningDuration, err = time.ParseDuration(opt.RunningDuration)
	if err != nil {
		return nil, err
	}

	return &e, nil
}

func flip(i int32) *int32 {
	return &i
}

// getTimes takes a suspendAt value and convert its value into minutes, and do
// the same with time.Now().
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
