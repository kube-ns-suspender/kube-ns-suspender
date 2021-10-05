package engine

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Watchlist []v1.Namespace

type Engine struct {
	Logger zerolog.Logger
	Mutex  sync.Mutex
	Wl     Watchlist
}

// New returns a new engine instance
func New() *Engine {
	e := Engine{
		Logger: zerolog.New(os.Stderr).With().Timestamp().Logger(),
	}
	return &e
}

// watcher periodically watches the namespaces, and add them to the engine
// watchlist if they have the 'kube-ns-suspender/desiredState' set.
func (eng *Engine) Watcher(ctx context.Context, cs *kubernetes.Clientset) {
	wlLogger := eng.Logger.With().Str("routine", "watcher").Logger()
	wlLogger.Info().Msg("watcher started")
	ctx, cancel := context.WithCancel(ctx)

	var id int
	for {
		wlLogger.Debug().Int("inventory id", id).Msg("starting new namespaces inventory")
		ns, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}) // TODO: think about adding a label to filter here
		if err != nil {
			cancel()
			wlLogger.Fatal().Err(err).Msg("cannot list namespaces")
		}

		eng.Mutex.Lock()
		// look for new namespaces to watch
		for _, n := range ns.Items {
			if _, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
				if !isNamespaceInWatchlist(n, eng.Wl) {
					eng.Wl = append(eng.Wl, n)
				}
			}
		}

		// remove unused namespaces
		eng.Wl = removeUnusedNamespaces(ns, eng.Wl)
		for _, v := range eng.Wl {
			fmt.Println(v.Name)
		}
		eng.Mutex.Unlock()
		wlLogger.Debug().Int("inventory id", id).Msg("namespaces inventory ended")
		id++
		time.Sleep(15 * time.Second)
	}
}

// isNamespaceInWatchlist checks if ns is already in the watchlist
func isNamespaceInWatchlist(ns v1.Namespace, wl Watchlist) bool {
	for _, n := range wl {
		if n.Name == ns.Name {
			return true
		}
	}
	return false
}

// removeUnusedNamespaces removes namespaces that are no longer in use from
// watchlist
func removeUnusedNamespaces(ns *v1.NamespaceList, wl Watchlist) Watchlist {
	for i, n := range wl {
		// if a ns is in the watchlist but no longer in the freshly scanned list,
		// drop it
		if !isNamespaceInWatchlist(n, ns.Items) {
			wl[i] = wl[len(wl)-1]
			wl = wl[:len(wl)-1]
		}
	}
	return wl
}
