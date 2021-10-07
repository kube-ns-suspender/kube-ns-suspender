package engine

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Watcher periodically watches the namespaces, and add them to the engine
// watchlist if they have the 'kube-ns-suspender/desiredState' set.
func (eng *Engine) Watcher(ctx context.Context, cs *kubernetes.Clientset) {
	eng.Mutex.Lock()
	wlLogger := eng.Logger.With().
		Str("routine", "watcher").Logger()
	eng.Mutex.Unlock()

	wlLogger.Info().Msg("watcher started")

	var id int
	for {
		wlLogger.Debug().Int("inventory id", id).
			Msg("starting new namespaces inventory")
		ns, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}) // TODO: think about adding a label to filter here
		if err != nil {
			wlLogger.Fatal().
				Err(err).
				Msg("cannot list namespaces")
		}

		eng.Mutex.Lock()
<<<<<<< HEAD
		// look for new namespaces to watch
		var wllen int
		for _, n := range ns.Items {
			if _, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
				eng.Wl <- n
				wllen++
			}
		}
		// update the watchlist length metric
		wlLogger.Debug().
			Msgf("channel length: %d", wllen)

		eng.MetricsServ.WatchlistLength.Set(float64(wllen))
		wlLogger.Debug().
			Int("inventory id", id).
			Msg("namespaces inventory ended")
=======
		// create fresh new variables for metrics
		var wllen, runningNs, suspendedNs, runningForcedNs, unknownNs int
		// look for new namespaces to watch
		for _, n := range ns.Items {
			if state, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
				eng.Wl <- n

				// increment variables for metrics
				wllen++
				switch state {
				case running:
					runningNs++
				case forced:
					runningForcedNs++
				case suspended:
					suspendedNs++
				default:
					unknownNs++
				}
			}
		}
		// update metrics
		wlLogger.Debug().Msgf("channel length: %d", wllen)
		eng.MetricsServ.WatchlistLength.Set(float64(wllen))

		wlLogger.Debug().Msgf("running namespaces: %d", runningNs)
		eng.MetricsServ.NumRunningNamspaces.Set(float64(runningNs))

		wlLogger.Debug().Msgf("suspended namespaces: %d", suspendedNs)
		eng.MetricsServ.NumSuspendedNamspaces.Set(float64(suspendedNs))

		wlLogger.Debug().Msgf("running forced namespaces: %d", runningForcedNs)
		eng.MetricsServ.NumRunningForcedNamspaces.Set(float64(runningForcedNs))

		wlLogger.Debug().Int("inventory id", id).Msg("namespaces inventory ended")
>>>>>>> main
		eng.Mutex.Unlock()
		id++
		time.Sleep(30 * time.Second)
	}
}
