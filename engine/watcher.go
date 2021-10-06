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
	wlLogger := eng.Logger.With().
		Str("routine", "watcher").Logger()
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
		// clean the watchlist
		// eng.Wl = Watchlist{}
		// look for new namespaces to watch
		for _, n := range ns.Items {
			if _, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
				// if !isNamespaceInWatchlist(n, eng.Wl) {
				// 	eng.Wl = append(eng.Wl, n)
				// }
				eng.Wl <- n
			}
		}
		// update the watchlist length metric
		wlLogger.Debug().
			Msgf("channel length: %d", len(eng.Wl))

		eng.MetricsServ.WatchlistLength.Set(float64(len(eng.Wl)))
		wlLogger.Debug().
			Int("inventory id", id).
			Msg("namespaces inventory ended")
		eng.Mutex.Unlock()
		id++
		time.Sleep(30 * time.Second)
	}
}
