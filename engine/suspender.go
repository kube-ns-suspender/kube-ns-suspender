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

// Suspender receives namespaces from Watcher and handles them. It means that
// it will read and write namespaces' annotations, and scale resources.
func (eng *Engine) Suspender(ctx context.Context, cs *kubernetes.Clientset) {
	eng.Mutex.Lock()
	eng.RunningNamespacesList = make(map[string]time.Time)
	eng.Logger.Info().Str("routine", "suspender").Msg("suspender started")
	eng.Mutex.Unlock()

	for {
		// wait for the next namespace to check
		n := <-eng.Wl
		start := time.Now()

		// we create a sublogger to avoid "namespace" field duplication at each
		// loop
		eng.Mutex.Lock()
		sLogger := eng.Logger.With().Str("routine", "suspender").Str("namespace", n.Name).Logger()
		eng.Mutex.Unlock()

		sLogger.Trace().Msgf("namespace %s received from watcher", n.Name)
		eng.Mutex.Lock()

		desiredState, ok := n.Annotations[eng.Options.Prefix+"desiredState"]
		if !ok {
			// the annotation does not exist, which means that it is the first
			// time we see this namespace. So by default, it should be "running"
			now, suspendAt, err := getTimes(n.Annotations[eng.Options.Prefix+"dailySuspendTime"])
			if err != nil {
				sLogger.Error().
					Err(err).
					Msgf("cannot parse dailySuspendTime time on namespace %s", n.Name)
			}
			sLogger.Trace().Msgf("now: %d, dailySuspendTime: %d, remaining: %s", now, suspendAt,
				time.Duration(suspendAt)*time.Minute-time.Duration(now)*time.Minute)
			if suspendAt <= now {
				// patch the namespace
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					res, err := cs.CoreV1().
						Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					res.Annotations[eng.Options.Prefix+"desiredState"] = suspended
					var updateOpts metav1.UpdateOptions
					// if the flag -dryrun is used, do not update resources
					if eng.Options.DryRun {
						updateOpts = metav1.UpdateOptions{
							DryRun: append(updateOpts.DryRun, "All"),
						}
					}
					_, err = cs.CoreV1().
						Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().
						Err(err).
						Msgf("cannot update namespace %s object", n.Name)
				} else {
					sLogger.Info().
						Msgf("suspended namespace %s based on daily suspend time", n.Name)
				}
				desiredState = suspended
			} else {
				eng.Mutex.Unlock()
				sLogger.Debug().Msgf("suspender loop for namespace %s duration: %s", n.Name, time.Since(start))
				continue
			}
		}

		switch desiredState {
		case running:
			if date, ok := eng.RunningNamespacesList[n.Name]; !ok {
				// first time we see this namespace as running, so we simply add
				// it to the map with the current time
				eng.RunningNamespacesList[n.Name] = time.Now().Local()

				// then we add the annotation that indicates when the namespace
				// will be automtically suspended again
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					res, err := cs.CoreV1().
						Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					res.Annotations[eng.Options.Prefix+"auto_nextSuspendTime"] = time.Now().
						Add(eng.RunningDuration).Format(time.Kitchen)
					var updateOpts metav1.UpdateOptions
					// if the flag -dryrun is used, do not update resources
					if eng.Options.DryRun {
						updateOpts = metav1.UpdateOptions{
							DryRun: append(updateOpts.DryRun, "All"),
						}
					}
					_, err = cs.CoreV1().
						Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().
						Err(err).
						Msgf("cannot add nextSuspendTime annotation to namespace %s", n.Name)
				} else {
					sLogger.Debug().
						Msgf("added nextSuspendTime annotation to %s on %s ", time.Now().
							Add(eng.RunningDuration).Format(time.Kitchen), n.Name)
				}
			} else {
				if time.Now().Local().Sub(date) < eng.RunningDuration {
					// we do not have to suspend the namespace yet, so we
					// continue
					eng.Mutex.Unlock()
					sLogger.Debug().Msgf("suspender loop for namespace %s duration: %s", n.Name, time.Since(start))
					continue
				}
				// if we end up here, it means that we have to suspend the
				// namespace as it as been running for too long
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					res, err := cs.CoreV1().
						Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					res.Annotations[eng.Options.Prefix+"desiredState"] = suspended
					delete(res.Annotations, eng.Options.Prefix+"auto_nextSuspendTime")

					var updateOpts metav1.UpdateOptions
					// if the flag -dryrun is used, do not update resources
					if eng.Options.DryRun {
						updateOpts = metav1.UpdateOptions{
							DryRun: append(updateOpts.DryRun, "All"),
						}
					}
					_, err = cs.CoreV1().
						Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().
						Err(err).
						Msgf("cannot update namespace %s object", n.Name)
				} else {
					sLogger.Info().
						Msgf("suspended namespace %s based on uptime", n.Name)
					desiredState = suspended
					delete(eng.RunningNamespacesList, n.Name)
				}
			}
		case suspended:
			// TODO: think about removing ²the annotation auto_nextSuspendTime
			// here. Elsewhere, it will stay until the next running call.
		default:
			sLogger.Error().
				Err(errors.New("state not recognised: "+desiredState)).
				Msgf("state %s is not recognised", desiredState)
			eng.Mutex.Unlock()
			sLogger.Debug().Msgf("suspender loop for namespace %s duration: %s", n.Name, time.Since(start))
			continue
		}

		// we do a switch again, but with the updated value
		// get deployments of the namespace
		deployments, err := cs.AppsV1().Deployments(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("object", "deployment").
				Msg("cannot list deployments")
		}

		// get cronjobs of the namespace
		cronjobs, err := cs.BatchV1beta1().CronJobs(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("object", "cronjob").
				Msg("cannot list cronjobs")
		}

		// get statefulsets of the namespace
		statefulsets, err := cs.AppsV1().StatefulSets(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().
				Err(err).
				Str("object", "statefulset").
				Msg("cannot list statefulsets")
		}
		var wg sync.WaitGroup
		switch desiredState {
		case running:
			wg.Add(3)
			// check and patch deployments
			go func() {
				if err := checkRunningDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.DryRun); err != nil {
					sLogger.Error().
						Err(err).
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
						Str("object", "statefulset").
						Msg("suspended steatfulsets conformity checks failed")
				}
				wg.Done()
			}()
		default:
			errMsg := fmt.Sprintf("state %s is not a supported state", desiredState)
			sLogger.Error().
				Err(errors.New(errMsg)).
				Msg("desired state cannot be recognised")
		}
		wg.Wait()
		// modify resources now based on n state
		eng.Mutex.Unlock()
		sLogger.Debug().Msgf("suspender loop for namespace %s duration: %s", n.Name, time.Since(start))
	}
}
