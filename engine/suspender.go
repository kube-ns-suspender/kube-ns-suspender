package engine

import (
	"context"
	"errors"
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

	for {
		eng.Mutex.Unlock()
		// wait for the next namespace to check
		n := <-eng.Wl
		start := time.Now()

		// we create a sublogger to avoid "namespace" field duplication at each
		// loop
		eng.Mutex.Lock()
		sLogger := eng.Logger.With().Str("routine", "suspender").Str("namespace", n.Name).Logger()
		sLogger.Trace().Msgf("namespace received from watcher")

		// get the namespace state
		dState := n.Annotations[eng.Options.Prefix+DesiredState]

		/*
			This first switch-case statement will ensure that the namespace has a state set.
			- if dState is empty, it means that it is the first time we see this namespace, so we
			add the annotation with the state Running
			- if dState is equal to Running or Suspended, the switch-case will do nothing.
			- if dState ends in the default case, it means that the state has not been recognised, so
			we have to error
		*/

		switch dState {
		case "":
			sLogger.Debug().Msgf("namespace has no %s annotation, it is probably the first time I see it", DesiredState)
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				// we set the annotation to running
				res.Annotations[eng.Options.Prefix+DesiredState] = Running

				_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
				return err
			}); err != nil {
				sLogger.Error().Err(err).Msgf("cannot update namespace object")
				// we give up and handle the next namespace
				sLogger.Debug().Msgf("suspender loop ended, duration: %s", time.Since(start))
				continue
			}
			sLogger.Debug().Msgf("added annotation %s=%s to namespace", DesiredState, Running)

			// we now update the value of dState to match the new namespace annotation
			dState = Running
		case Running, Suspended:
			// do nothing
		default:
			sLogger.Error().Err(errors.New("state not recognised: "+dState)).Msgf("state %s is not recognised", dState)
			// we give up and handle the next namespace
			sLogger.Debug().Msgf("suspender loop ended, duration: %s", time.Since(start))
			continue
		}

		/*
			In order to be able to edit the resources, we first need to get all of them from
			the namespace. This is the goal of the following expressions
		*/

		// get deployments of the namespace
		deployments, err := cs.AppsV1().Deployments(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().Err(err).Msg("cannot list deployments")
		}

		// get cronjobs of the namespace
		cronjobs, err := cs.BatchV1beta1().CronJobs(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().Err(err).Msg("cannot list cronjobs")
		}

		// get statefulsets of the namespace
		statefulsets, err := cs.AppsV1().StatefulSets(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().Err(err).Msg("cannot list statefulsets")
		}

		/*
			If we end up here, it means that:
			- the namespace has a desiredState annotation
			- the annotation is valid

			Now, we have to do another switch-case statement to manage the behavior of
			the underlying replicas.
			This switch-case will match dState again, with different behaviors:
			- if dState == Suspended:
				* be sure that the undelying resources are suspended. If not, downscale them

			- if dState == Running:
				* check if the namespace should be suspended, based on the dailySuspendTime annotation. If it should:
					1. update dState to Suspended
					2. update the namespace annotation to Suspended
					3. go to the beginning of the switch-case: this will re-evaluate everything, and downscale as it
					   will end in the dState == Suspended case.

				* check if the namespace should be suspended, based on the nextSuspendTime annotation. If it should:
					1. we do the same as for dailySuspendTime annotation

				* check if the namespace is correctly Running, as the annotation might have been set manually. If not,
				  upscale everything

			Also, if the namespace is running, we need to ensure that the annotation DailySuspendTime is set.
			If not, the namespace will never be suspended at a given time.

			To do so, we have two integers:
			- now, which contains today's time
			- suspendAt, which contains today's suspend time for the namespace
		*/

		var now, suspendAt int
	LOOP:
		sLogger.Debug().Msgf("namespace is seen as being %s", dState)
		switch dState {
		case Suspended:
			// the checks will be done concurrently to optimise verification duration
			var wg sync.WaitGroup
			wg.Add(3)
			// check and patch deployments
			go func() {
				if err := checkSuspendedDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.Prefix); err != nil {
					sLogger.Error().Err(err).Str("object", "deployment").Msg("suspended conformity checks failed")
				}
				wg.Done()
			}()

			// check and patch cronjobs
			go func() {
				if err := checkSuspendedCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("object", "cronjob").Msg("suspended cronjobs conformity checks failed")
				}
				wg.Done()
			}()

			// check and patch statefulsets
			go func() {
				if err := checkSuspendedStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name, eng.Options.Prefix); err != nil {
					sLogger.Error().Err(err).Str("object", "statefulset").Msg("suspended steatfulsets conformity checks failed")
				}
				wg.Done()
			}()
			// we wait for all the checks to be done
			wg.Wait()
		case Running:
			// we ensure that the annotation DailySuspendTime is set
			now, suspendAt, err = getTimes(n.Annotations[eng.Options.Prefix+DailySuspendTime])
			if err != nil {
				sLogger.Warn().Err(err).Msgf("cannot parse %s annotation on namespace", DailySuspendTime)
				sLogger.Debug().Msgf("suspender loop ended, duration: %s", time.Since(start))
			}

			// check if dailySuspendTime is set and past
			if err == nil && suspendAt <= now {
				sLogger.Debug().Msgf("%s is less or equal to now (value: %d, now: %d), updating annotation to %s", DailySuspendTime, suspendAt, now, Suspended)
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					// we set the annotation to suspended
					res.Annotations[eng.Options.Prefix+DesiredState] = Suspended

					_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().Err(err).Msgf("cannot update namespace object")
					// we give up and handle the next namespace
					sLogger.Debug().Msgf("suspender loop ended, duration: %s", time.Since(start))
					continue
				} else {
					sLogger.Debug().Msgf("added annotation %s=%s to namespace, going back to the start of the switch-case", DesiredState, Suspended)

					// we now update the value of dState to match the new namespace annotation
					dState = Suspended

					// re-evaluate the switch-case, to end in the dState == Suspended case
					// and downscale everything
					goto LOOP
				}
			}

			// check if nextSuspendTime exists and is past
			if val, ok := n.Annotations[eng.Options.Prefix+nextSuspendTime]; ok {
				nextSuspendAt, err := time.Parse(time.RFC822Z, val)
				if err != nil {
					sLogger.Error().Err(err).Msgf("cannot parse %s value %s in time format %s for namespace", nextSuspendTime, val, time.RFC822Z)
					continue
				}

				if time.Now().Local().Sub(nextSuspendAt) <= eng.RunningDuration {
					sLogger.Debug().Msgf("%s is less or equal to now (value: %d, now: %d), updating annotation to %s", nextSuspendTime, suspendAt, now, Suspended)
					if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						// we set the annotation to suspended
						res.Annotations[eng.Options.Prefix+DesiredState] = Suspended

						_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
						return err
					}); err != nil {
						sLogger.Error().Err(err).Msgf("cannot update namespace object")
						// we give up and handle the next namespace
						sLogger.Debug().Msgf("suspender loop ended, duration: %s", time.Since(start))
						continue
					} else {
						sLogger.Debug().Msgf("added annotation %s=%s to namespace, going back to the start of the switch-case", DesiredState, Suspended)

						// we now update the value of dState to match the new namespace annotation
						dState = Suspended

						// re-evaluate the switch-case, to end in the dState == Suspended case
						// and downscale everything
						goto LOOP
					}
				}
			}

			/*
				If we end up here, it means that the namespace should be running. All we have to do now is to
				run the conformity checks on the namespace resources.

				The checks will be done concurrently to optimise verification duration.
				We also grab the hasBeenPatched bool for each check. If this value is set to true anywhere, it means
				that the namespace has been unsuspended manually (state == running but everything is scaled to 0).
				To detect this, each resource will increment a counter called patchedResourcesCounter by one.
				If at the end the counter is > than 0, we add the annotation to the namespace.
			*/
			var wg sync.WaitGroup
			var patchedResourcesCounter int
			wg.Add(3)
			// check and patch deployments
			go func() {
				hasBeenPatched, err := checkRunningDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.Prefix)
				if err != nil {
					sLogger.Error().Err(err).Msg("running deployments conformity checks failed")
				}
				if hasBeenPatched {
					patchedResourcesCounter++
				}
				wg.Done()
			}()

			// check and patch cronjobs
			go func() {
				hasBeenPatched, err := checkRunningCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name)
				if err != nil {
					sLogger.Error().Err(err).Msg("running cronjobs conformity checks failed")
				}
				if hasBeenPatched {
					patchedResourcesCounter++
				}
				wg.Done()
			}()

			// check and patch statefulsets
			go func() {
				hasBeenPatched, err := checkRunningStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name, eng.Options.Prefix)
				if err != nil {
					sLogger.Error().Err(err).Msg("running steatfulsets conformity checks failed")
				}
				if hasBeenPatched {
					patchedResourcesCounter++
				}
				wg.Done()
			}()
			// we wait for all the checks to be done
			wg.Wait()

			// now we can check if patchedResourcesCounter is > 0 and add nextSuspendTime depending of the result
			if patchedResourcesCounter > 0 {
				sLogger.Debug().Msgf("it seems that namespace has been unsuspended manually, so I am adding the annotation %s to it", n.Name, nextSuspendTime)
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					/*
						The time format used for this annotation is RFC822Z:
							02 Jan 06 15:04 -0700

						No need to use a kitchen format as this date should not be manually edited.
						However, it makes it easier to detect if the date is passed, as it returns
						a complete date, not only the hours and minutes of the day.
					*/
					res.Annotations[eng.Options.Prefix+nextSuspendTime] = time.Now().Local().
						Add(eng.RunningDuration).Format(time.RFC822Z)

					_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().Err(err).Msgf("cannot add %s annotation to namespace", nextSuspendTime, n.Name)
				}
			}
		}

		sLogger.Debug().Msgf("suspender loop ended, duration: %s", time.Since(start))
	}
}
