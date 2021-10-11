package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

func (eng *Engine) Suspender(ctx context.Context, cs *kubernetes.Clientset) {
	eng.Mutex.Lock()
	sLogger := eng.Logger.With().
		Str("routine", "suspender").Logger()
	eng.RunningForcedHistory = make(map[string]time.Time)
	eng.Mutex.Unlock()

	sLogger.Info().
		Msg("suspender started")

	for {
		// wait for the next namespace to check
		n := <-eng.Wl
		eng.Mutex.Lock()

		if suspendAt, ok := n.Annotations["kube-ns-suspender/suspendAt"]; ok {
			// suspendAt is specified, so we need to check if we have to suspend
			// the namespace
			now, suspend, err := getTimes(suspendAt)
			if err != nil {
				sLogger.Fatal().
					Err(err).
					Str("namespace", n.Name).
					Msg("cannot parse suspend time")
			}

			if n.Annotations["kube-ns-suspender/desiredState"] == running && suspend <= now {
				// patch the namespace
				err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					result, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					result.Annotations["kube-ns-suspender/desiredState"] = suspended
					var updateOpts metav1.UpdateOptions
					// if the flag -dryrun is used, do not update resources
					if eng.Options.DryRun {
						updateOpts = metav1.UpdateOptions{
							DryRun: append(updateOpts.DryRun, "All"),
						}
					}
					_, err = cs.CoreV1().Namespaces().Update(ctx, result, updateOpts)
					return err
				})
				if err != nil {
					sLogger.Fatal().
						Err(err).
						Str("namespace", n.Name).
						Msg("cannot update namespace object")
				}
				sLogger.Info().
					Str("namespace", n.Name).
					Msgf("suspended namespace %s based on suspend time", n.Name)
				n.Annotations["kube-ns-suspender/desiredState"] = suspended

			}
		}

		if n.Annotations["kube-ns-suspender/desiredState"] == forced {
			if creationTime, ok := eng.RunningForcedHistory[n.Name]; ok {
				if time.Since(creationTime.Local()) >= 10*time.Minute {
					// suspend the namespace
					err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						result, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						result.Annotations["kube-ns-suspender/desiredState"] = suspended
						var updateOpts metav1.UpdateOptions
						// if the flag -dryrun is used, do not update resources
						if eng.Options.DryRun {
							updateOpts = metav1.UpdateOptions{
								DryRun: append(updateOpts.DryRun, "All"),
							}
						}
						_, err = cs.CoreV1().Namespaces().Update(ctx, result, metav1.UpdateOptions{})
						return err
					})
					if err != nil {
						sLogger.Fatal().
							Err(err).
							Str("namespace", n.Name).
							Msg("cannot update namespace object")
					}
					n.Annotations["kube-ns-suspender/desiredState"] = suspended

					// remove the namespace from the map
					delete(eng.RunningForcedHistory, n.Name)
					sLogger.Info().
						Str("namespace", n.Name).
						Msgf("suspended namespace %s based on uptime", n.Name)
				}
			} else {
				eng.RunningForcedHistory[n.Name] = time.Now().Local()
				sLogger.Info().
					Str("namespace", n.Name).
					Msgf("unpausing %s", n.Name)
				sLogger.Info().
					Str("namespace", n.Name).
					Msgf("%s will be automatically suspended at %s", n.Name, eng.RunningForcedHistory[n.Name].Add(30*time.Minute))
			}
		}

		// get the namespace desired status
		desiredState := n.Annotations["kube-ns-suspender/desiredState"]

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

		var wg sync.WaitGroup
		switch desiredState {
		case running, forced:
			wg.Add(3)
			// check and patch deployments
			go func() {
				if err := checkRunningDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
						Str("namespace", n.Name).
						Str("object", "deployment").
						Msg("running deployments conformity checks failed")
				}
				wg.Done()
			}()

			// check and patch cronjobs
			go func() {
				if err := checkRunningCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
						Str("namespace", n.Name).
						Str("object", "cronjob").
						Msg("running cronjobs conformity checks failed")
				}
				wg.Done()
			}()

			// check and patch statefulsets
			go func() {
				if err := checkRunningStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
						Str("namespace", n.Name).
						Str("object", "statefulset").
						Msg("running steatfulsets conformity checks failed")
				}
				wg.Done()
			}()
		case suspended:
			wg.Add(3)
			// check and patch deployments
			go func() {
				if err := checkSuspendedDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
						Str("namespace", n.Name).
						Str("object", "deployment").
						Msg("suspended conformity checks failed")
				}
				wg.Done()
			}()

			// check and patch cronjobs
			go func() {
				if err := checkSuspendedCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
						Str("namespace", n.Name).
						Str("object", "cronjob").
						Msg("suspended cronjobs conformity checks failed")
				}
				wg.Done()
			}()

			// check and patch statefulsets
			go func() {
				if err := checkSuspendedStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
						Str("namespace", n.Name).
						Str("object", "statefulset").
						Msg("suspended steatfulsets conformity checks failed")
				}
				wg.Done()
			}()
		default:
			errMsg := fmt.Sprintf("state %s is not a supported state", desiredState)
			sLogger.Error().
				Err(errors.New(errMsg)).
				Str("namespace", n.Name).
				Msg("desired state cannot be recognised")
		}
		wg.Wait()
		eng.Mutex.Unlock()
	}
}
