package engine

import (
	"context"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	running   = "Running"
	suspended = "Suspended"
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

	var id int
	for {
		wlLogger.Debug().Int("inventory id", id).Msg("starting new namespaces inventory")
		ns, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}) // TODO: think about adding a label to filter here
		if err != nil {
			wlLogger.Fatal().Err(err).Msg("cannot list namespaces")
		}

		eng.Mutex.Lock()
		// clean the watchlist
		eng.Wl = Watchlist{}
		// look for new namespaces to watch
		for _, n := range ns.Items {
			if _, ok := n.Annotations["kube-ns-suspender/desiredState"]; ok {
				if !isNamespaceInWatchlist(n, eng.Wl) {
					eng.Wl = append(eng.Wl, n)
				}
			}
		}

		// debug: on
		// for _, v := range eng.Wl {
		// 	fmt.Println(v.Name)
		// }
		// debug: off
		eng.Mutex.Unlock()
		wlLogger.Debug().Int("inventory id", id).Msg("namespaces inventory ended")
		id++
		time.Sleep(15 * time.Second)
	}
}

// TODO: is it necessary to use the same engine? Could a simple copy be enough?
func (eng *Engine) Suspender(ctx context.Context, cs *kubernetes.Clientset) {
	sLogger := eng.Logger.With().Str("routine", "suspender").Logger()
	sLogger.Info().Msg("suspender started")
	// wait a bit for the watcher to populate the first watchlist
	time.Sleep(100 * time.Millisecond)

	for {
		// we do a copy of the watchlist to avoid blocking the mutexes for too
		// long. If any modification is done to the original watchlist, the
		// changes will occur in the next iteration
		eng.Mutex.Lock()
		for _, n := range eng.Wl {
			// get the namespace desired status
			desiredState := n.Annotations["kube-ns-suspender/desiredState"]
			// debug: on
			// fmt.Printf("namespace %s status: %s\n", n.Name, desiredState)
			// debug: off

			// get the deployments of the namespace
			deployments, err := cs.AppsV1().Deployments(n.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				sLogger.Fatal().Err(err).Str("namespace", n.Name).Msg("cannot list deployments")
			}
			// debug: on
			// fmt.Printf("namespace: %s\n", n.Name)
			// for _, d := range deployments.Items {
			// 	fmt.Println(d.Name)
			// }
			// debug: off

			switch desiredState {
			case running:
				if err := checkRunningConformity(ctx, sLogger, deployments.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Msg("running conformity checks failed")
				}
			case suspended:
				if err := checkSuspendedConformity(ctx, sLogger, deployments.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Msg("suspended conformity checks failed")
				}
			default:
			}
		}
		eng.Mutex.Unlock()
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

// checkRunningConformity verifies that all deployments within the namespace are
// currently running
func checkRunningConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns string) error {
	for _, d := range deployments {
		// debug: on
		if d.Name == "kube-ns-suspender-depl" {
			continue
		}
		// debug: off
		repl := int(*d.Spec.Replicas)
		if repl == 0 {
			// get the desired number of replicas
			repl, err := strconv.Atoi(d.Annotations["kube-ns-suspender/originalReplicas"])
			if err != nil {
				return err
			}

			l.Debug().Str("namespace", ns).Str("deployment", d.Name).Msgf("scaling %s from 0 to %d replicas", d.Name, repl)
			// patch the deployment
			if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, repl); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSuspendedConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns string) error {
	for _, d := range deployments {
		repl := int(*d.Spec.Replicas)
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Debug().Str("namespace", ns).Str("deployment", d.Name).Msgf("scaling %s from %d to 0 replicas", d.Name, repl)
			// patch the deployment
			if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchDeploymentReplicas updates the number of replicas of a given deployment
func patchDeploymentReplicas(ctx context.Context, cs *kubernetes.Clientset, ns, d string, repl int) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.AppsV1().Deployments(ns).Get(context.TODO(), d, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Replicas = flip(int32(repl))
		_, err = cs.AppsV1().Deployments(ns).Update(context.TODO(), result, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

func flip(i int32) *int32 {
	return &i
}
