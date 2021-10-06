package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

func (eng *Engine) Suspender(ctx context.Context, cs *kubernetes.Clientset) {
	sLogger := eng.Logger.With().
		Str("routine", "suspender").Logger()
	sLogger.Info().
		Msg("suspender started")
	// wait a bit for the watcher to populate the first watchlist
	time.Sleep(100 * time.Millisecond)

	for {
		// wait for the next namespace to check
		n := <-eng.Wl
		eng.Mutex.Lock()

		if suspendAt, ok := n.Annotations["kube-ns-suspender/suspendAt"]; ok {
			// suspendAt is specified, so we need to check if we have to suspend
			// the namespace
			now, suspend, err := getTimes(suspendAt, eng.TZ)
			if err != nil {
				sLogger.Fatal().
					Err(err).
					Str("namespace", n.Name).
					Msg("cannot parse suspend time")
			}

			if suspend <= now && n.Annotations["kube-ns-suspender/desiredStatus"] != suspended {
				// patch the namespace
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					result, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					result.Annotations["kube-ns-suspender/desiredState"] = suspended
					_, err = cs.CoreV1().Namespaces().Update(ctx, result, metav1.UpdateOptions{})
					return err
				})
				if err != nil {
					sLogger.Fatal().
						Err(err).
						Str("namespace", n.Name).
						Msg("cannot get namespace object")
				}
				sLogger.Info().
					Str("namespace", n.Name).
					Msgf("suspended namespace %s based on suspend time", n.Name)

			}
		}

		// get the namespace desired status
		var desiredState string
		ns, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("namespace", n.Name).
				Msg("cannot get namespace object")
			// use non updated value, loss one loop but don't crash
			desiredState = n.Annotations["kube-ns-suspender/desiredState"]
		} else {
			// use updated value
			desiredState = ns.Annotations["kube-ns-suspender/desiredState"]
		}

		// get deployments of the namespace
		deployments, err := cs.AppsV1().Deployments(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("namespace", n.Name).
				Str("object", "deployment").
				Msg("cannot list deployments")
		}

		// get cronjobs of the namespace
		cronjobs, err := cs.BatchV1beta1().CronJobs(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("namespace", n.Name).
				Str("object", "cronjob").
				Msg("cannot list cronjobs")
		}

		// get statefulsets of the namespace
		statefulsets, err := cs.AppsV1().StatefulSets(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("namespace", n.Name).
				Str("object", "statefulset").
				Msg("cannot list statefulsets")
		}

		// TODO: add sync.WaitGroup here to // the work on each kind of object
		switch desiredState {
		case running:
			// check and patch deployments
			if err := checkRunningDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name); err != nil {
				sLogger.Error().
					Err(err).
					Str("namespace", n.Name).
					Str("object", "deployment").
					Msg("running deployments conformity checks failed")
			}
			// check and patch cronjobs
			if err := checkRunningCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name); err != nil {
				sLogger.Error().
					Err(err).
					Str("namespace", n.Name).
					Str("object", "cronjob").
					Msg("running cronjobs conformity checks failed")
			}
			// check and patch statefulsets
			if err := checkRunningStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name); err != nil {
				sLogger.Error().
					Err(err).
					Str("namespace", n.Name).
					Str("object", "statefulset").
					Msg("running steatfulsets conformity checks failed")
			}
		case suspended:
			// check and patch deployments
			if err := checkSuspendedDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name); err != nil {
				sLogger.Error().
					Err(err).
					Str("namespace", n.Name).
					Str("object", "deployment").
					Msg("suspended conformity checks failed")
			}
			// check and patch cronjobs
			if err := checkSuspendedCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name); err != nil {
				sLogger.Error().
					Err(err).
					Str("namespace", n.Name).
					Str("object", "cronjob").
					Msg("suspended cronjobs conformity checks failed")
			}
			// check and patch statefulsets
			if err := checkSuspendedStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name); err != nil {
				sLogger.Error().
					Err(err).
					Str("namespace", n.Name).
					Str("object", "statefulset").
					Msg("suspended steatfulsets conformity checks failed")
			}
		default:
			errMsg := fmt.Sprintf("state %s is not a supported state", desiredState)
			sLogger.Error().
				Err(errors.New(errMsg)).
				Str("namespace", n.Name).
				Msg("desired state cannot be recognised")
		}
		eng.Mutex.Unlock()
		time.Sleep(15 * time.Second)
	}
}
