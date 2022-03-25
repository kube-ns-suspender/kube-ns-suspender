package engine

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Watcher periodically watches the namespaces, and add them to the engine
// watchlist if they have the 'kube-ns-suspender/DesiredState' set.
func (eng *Engine) Watcher(ctx context.Context, cs *kubernetes.Clientset) {
	eng.Mutex.Lock()
	wLogger := eng.Logger.With().
		Str("routine", "watcher").Logger()
	eng.Mutex.Unlock()

	wLogger.Info().Msg("watcher started")

	var id int
	for {
		start := time.Now()
		wLogger.Debug().Int("inventory_id", id).
			Msg("starting new namespaces inventory")

		ns, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}) // TODO: think about adding a label to filter here
		if err != nil {
			wLogger.Fatal().
				Err(err).
				Msg("cannot list namespaces")
		}

		eng.Mutex.Lock()
		// create fresh new variables for metrics
		var wllen, runningNs, suspendedNs, unknownNs int

		// look for new namespaces to watch
		wLogger.Debug().Msgf("parsing namespaces list")
		for _, n := range ns.Items {
			if value, ok := n.Annotations[eng.Options.Prefix+controllerName]; ok {
				wLogger.Debug().Msgf("found annotation '%s'", eng.Options.Prefix+controllerName)
				if value == eng.Options.ControllerName {
					eng.Wl <- n
					wLogger.Debug().Msgf("annotation '%s' matches controller name (%s)", eng.Options.Prefix+controllerName, eng.Options.ControllerName)
					wLogger.Debug().Msgf("namespace %s sent to suspender", n.Name)
					wllen++

					// try to get the desiredState annotation
					if state, ok := n.Annotations[eng.Options.Prefix+DesiredState]; ok {
						// increment variables for metrics
						switch state {
						case Running:
							runningNs++
						case Suspended:
							suspendedNs++
						default:
							unknownNs++
						}
					} else {
						wLogger.Warn().Msgf("annotation '%s' not found", eng.Options.Prefix+DesiredState)
					}
				}
			}
		}

		// update metrics
		wLogger.Debug().Msgf("Metric - channel length: %d", wllen)
		eng.MetricsServ.WatchlistLength.Set(float64(wllen))

		wLogger.Debug().Msgf("Metric - running namespaces: %d", runningNs)
		eng.MetricsServ.NumRunningNamspaces.Set(float64(runningNs))

		wLogger.Debug().Msgf("Metric - suspended namespaces: %d", suspendedNs)
		eng.MetricsServ.NumSuspendedNamspaces.Set(float64(suspendedNs))

		wLogger.Debug().Msgf("Metric - unknown namespaces: %d", unknownNs)
		eng.MetricsServ.NumUnknownNamespaces.Set(float64(unknownNs))

		eng.Mutex.Unlock()

		// Question: Why not add `Int("inventory_id", id)` to every log line ?
		wLogger.Debug().Int("inventory_id", id).Msg("namespaces inventory ended")
		wLogger.Debug().Int("inventory_id", id).Msgf("inventory duration: %s", time.Since(start))

		id++
		time.Sleep(time.Duration(eng.Options.WatcherIdle) * time.Second)
	}
}
