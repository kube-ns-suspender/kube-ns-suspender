package metrics

import (
	"net/http"
	"time"

	"github.com/govirtuo/kube-ns-suspender/handlers"
	"github.com/prometheus/client_golang/prometheus"
)

// Server is the metrics server. It contains all the Prometheus metrics
type Server struct {
	Addr                  string
	NotRespondingList     map[string]bool
	WatchlistLength       prometheus.Gauge
	Uptime                prometheus.Gauge
	NumRunningNamspaces   prometheus.Gauge
	NumSuspendedNamspaces prometheus.Gauge
	NumUnknownNamespaces  prometheus.Gauge
}

// Init initializes the metrics
func Init() *Server {
	s := Server{
		Uptime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ns_suspender_uptime_sec",
			Help: "kube-ns-suspender uptime, in seconds.",
		}),
		WatchlistLength: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ns_suspender_watchlist_length",
			Help: "Number of namespaces that are in the watchlist",
		}),
		NumRunningNamspaces: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ns_suspender_running_namespaces",
			Help: "Number of namespaces that have the desired state 'Running'",
		}),
		NumSuspendedNamspaces: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ns_suspender_suspended_namespaces",
			Help: "Number of namespaces that have the desired state 'Suspended'",
		}),
		NumUnknownNamespaces: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kube_ns_suspender_unknown_namespaces",
			Help: "Number of namespaces that have an unknown state",
		}),
	}

	prometheus.MustRegister(
		s.Uptime,
		s.WatchlistLength,
		s.NumRunningNamspaces,
		s.NumSuspendedNamspaces,
	)

	// Start uptime counter
	go s.uptimeCounter()

	return &s
}

// Start starts the prometheus server
func (s *Server) Start() error {
	srv := &http.Server{
		Addr:         ":2112",
		Handler:      handlers.HandleFunc(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// goroutine to update the Uptime metric
func (s *Server) uptimeCounter() {
	for {
		s.Uptime.Add(5)
		time.Sleep(5 * time.Second)
	}
}
