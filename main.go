package main

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
	"k8s.io/client-go/rest"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
)

type watchlist []v1.Namespace

type engine struct {
	logger zerolog.Logger
	m      sync.Mutex
	wl     watchlist
}

func main() {
	var eng = engine{
		logger: zerolog.New(os.Stderr).With().Timestamp().Logger(),
	}
	eng.logger.Info().Msg("kube-ns-suspender launched")

	// create the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		eng.logger.Fatal().Err(err).Msg("cannot create in-cluster configuration")
	}
	eng.logger.Debug().Msg("in-cluster configuration successfully created")

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		eng.logger.Fatal().Err(err).Msg("cannot create the clientset")
	}
	eng.logger.Debug().Msg("clientset successfully created")

	ctx := context.Background()
	go eng.watcher(ctx, clientset)

	// wait forever
	select {}
}

// watcher periodically watches the namespaces, and add them to the engine
// watchlist if they have the 'kube-ns-suspender/desiredState' set.
func (eng *engine) watcher(ctx context.Context, cs *kubernetes.Clientset) {
	wlLogger := eng.logger.With().Str("routine", "watcher").Logger()
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

		eng.m.Lock()
		// look for new namespaces to watch
		for _, n := range ns.Items {
			if _, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
				if !isNamespaceInWatchlist(n, eng.wl) {
					eng.wl = append(eng.wl, n)
				}
			}
		}

		// remove unused namespaces
		eng.wl = removeUnusedNamespaces(ns, eng.wl)
		for _, v := range eng.wl {
			fmt.Println(v.Name)
		}
		eng.m.Unlock()
		wlLogger.Debug().Int("inventory id", id).Msg("namespaces inventory ended")
		id++
		time.Sleep(15 * time.Second)
	}
}

// isNamespaceInWatchlist checks if ns is already in the watchlist
func isNamespaceInWatchlist(ns v1.Namespace, wl watchlist) bool {
	for _, n := range wl {
		if n.Name == ns.Name {
			return true
		}
	}
	return false
}

// removeUnusedNamespaces removes namespaces that are no longer in use from
// watchlist
func removeUnusedNamespaces(ns *v1.NamespaceList, wl watchlist) watchlist {
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
