package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/rs/zerolog"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/batch/v1beta1"
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
	Logger      zerolog.Logger
	Mutex       sync.Mutex
	Wl          Watchlist
	MetricsServ metrics.Server
}

// New returns a new engine instance
func New(loglvl string) (*Engine, error) {
	e := Engine{
		Logger: zerolog.New(os.Stderr).With().Timestamp().Logger(),
	}

	lvl, err := zerolog.ParseLevel(loglvl)
	if err != nil {
		return nil, err
	}
	e.Logger = e.Logger.Level(lvl)
	return &e, nil
}

// Watcher periodically watches the namespaces, and add them to the engine
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
		// update the watchlist lenght metric
		eng.MetricsServ.WatchlistLenght.Set(float64(len(eng.Wl)))
		wlLogger.Debug().Int("inventory id", id).Msg("namespaces inventory ended")
		eng.Mutex.Unlock()
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
		eng.Mutex.Lock()
		for _, n := range eng.Wl {
			// get the namespace desired status
			desiredState := n.Annotations["kube-ns-suspender/desiredState"]

			// get deployments of the namespace
			deployments, err := cs.AppsV1().Deployments(n.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				sLogger.Fatal().Err(err).Str("namespace", n.Name).Str("object", "deployment").Msg("cannot list deployments")
			}

			// get cronjobs of the namespace
			cronjobs, err := cs.BatchV1beta1().CronJobs(n.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				sLogger.Fatal().Err(err).Str("namespace", n.Name).Str("object", "cronjob").Msg("cannot list cronjobs")
			}

			// get statefulsets of the namespace
			statefulsets, err := cs.AppsV1().StatefulSets(n.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				sLogger.Fatal().Err(err).Str("namespace", n.Name).Str("object", "statefulset").Msg("cannot list statefulsets")
			}

			// TODO: add sync.WaitGroup here to // the work on each kind of object
			switch desiredState {
			case running:
				// check and patch deployments
				if err := checkRunningDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("namespace", n.Name).Str("object", "deployment").Msg("running deployments conformity checks failed")
				}
				// check and patch cronjobs
				if err := checkRunningCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("namespace", n.Name).Str("object", "cronjob").Msg("running cronjobs conformity checks failed")
				}
				// check and patch statefulsets
				if err := checkRunningStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("namespace", n.Name).Str("object", "statefulset").Msg("running steatfulsets conformity checks failed")
				}
			case suspended:
				// check and patch deployments
				if err := checkSuspendedDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Msg("suspended conformity checks failed")
				}
				// check and patch cronjobs
				if err := checkSuspendedCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("namespace", n.Name).Str("object", "cronjob").Msg("suspended cronjobs conformity checks failed")
				}
				// check and patch statefulsets
				if err := checkSuspendedStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("namespace", n.Name).Str("object", "statefulset").Msg("suspended steatfulsets conformity checks failed")
				}
			default:
				errMsg := fmt.Sprintf("state %s is not a supported state", desiredState)
				sLogger.Error().Err(errors.New(errMsg)).Msg("desired state cannot be recognised")
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

// checkRunningDeploymentsConformity verifies that all deployments within the namespace are
// currently running
func checkRunningDeploymentsConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns string) error {
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

func checkSuspendedDeploymentsConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns string) error {
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

func checkRunningCronjobsConformity(ctx context.Context, l zerolog.Logger, cronjobs []v1beta1.CronJob, cs *kubernetes.Clientset, ns string) error {
	for _, c := range cronjobs {
		if *c.Spec.Suspend {
			l.Debug().Str("namespace", ns).Str("cronjob", c.Name).Msgf("updating %s from suspend: true to suspend: false", c.Name)
			if err := patchCronjobSuspend(ctx, cs, ns, c.Name, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSuspendedCronjobsConformity(ctx context.Context, l zerolog.Logger, cronjobs []v1beta1.CronJob, cs *kubernetes.Clientset, ns string) error {
	for _, c := range cronjobs {
		if !*c.Spec.Suspend {
			l.Debug().Str("namespace", ns).Str("cronjob", c.Name).Msgf("updating %s from suspend: false to suspend: true", c.Name)
			if err := patchCronjobSuspend(ctx, cs, ns, c.Name, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkRunningStatefulsetsConformity(ctx context.Context, l zerolog.Logger, statefulsets []appsv1.StatefulSet, cs *kubernetes.Clientset, ns string) error {
	for _, ss := range statefulsets {
		repl := int(*ss.Spec.Replicas)
		if repl == 0 {
			// get the desired number of replicas
			repl, err := strconv.Atoi(ss.Annotations["kube-ns-suspender/originalReplicas"])
			if err != nil {
				return err
			}

			l.Debug().Str("namespace", ns).Str("statefulset", ss.Name).Msgf("scaling %s from 0 to %d replicas", ss.Name, repl)
			// patch the statefulset
			if err := patchStatefulsetReplicas(ctx, cs, ns, ss.Name, repl); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSuspendedStatefulsetsConformity(ctx context.Context, l zerolog.Logger, statefulsets []appsv1.StatefulSet, cs *kubernetes.Clientset, ns string) error {
	for _, ss := range statefulsets {
		repl := int(*ss.Spec.Replicas)
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Debug().Str("namespace", ns).Str("statefulset", ss.Name).Msgf("scaling %s from %d to 0 replicas", ss.Name, repl)
			// patch the deployment
			if err := patchStatefulsetReplicas(ctx, cs, ns, ss.Name, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchDeploymentReplicas updates the number of replicas of a given deployment
func patchDeploymentReplicas(ctx context.Context, cs *kubernetes.Clientset, ns, d string, repl int) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.AppsV1().Deployments(ns).Get(ctx, d, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Replicas = flip(int32(repl))
		_, err = cs.AppsV1().Deployments(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

// patchCronjobSuspend updates the suspend state of a giver cronjob
func patchCronjobSuspend(ctx context.Context, cs *kubernetes.Clientset, ns, c string, suspend bool) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.BatchV1beta1().CronJobs(ns).Get(ctx, c, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Suspend = &suspend
		_, err = cs.BatchV1beta1().CronJobs(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}
	return nil
}

// patchStatefulsetSuspend updates the number of replicas of a given statefulset
func patchStatefulsetReplicas(ctx context.Context, cs *kubernetes.Clientset, ns, ss string, repl int) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.AppsV1().StatefulSets(ns).Get(ctx, ss, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Replicas = flip(int32(repl))
		_, err = cs.AppsV1().StatefulSets(ns).Update(ctx, result, metav1.UpdateOptions{})
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
