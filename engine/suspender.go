package engine

import (
	"context"
	"errors"
	"time"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// Suspender receives namespaces from Watcher and handles them. It means that
// it will read and write namespaces' annotations, and scale resources.
func (eng *Engine) Suspender(ctx context.Context, cs *kubernetes.Clientset, kedacs *v1alpha1.KedaV1alpha1Client) {
	// eng.Mutex.Lock()
	eng.Logger.Info().Str("routine", "suspender").Msg("suspender started")
	defer func() {
		eng.Logger.Fatal().Str("routine", "suspender").Msg("suspender exited")
	}()

	var stepName string
	for {
		// eng.Mutex.Unlock()

		// wait for the next namespace to check
		n := <-eng.Wl
		start := time.Now()

		// we create a sublogger to avoid "namespace" field duplication at each loop
		// eng.Mutex.Lock()
		sLogger := eng.Logger.With().Str("routine", "suspender").Str("namespace", n.Name).Logger()
		sLogger.Debug().Msg("namespace received from watcher")

		/*
			Step 1

			This first switch-case statement will ensure that the namespace has a state set.

			- if dState is empty, it means that it is the first time we see this namespace, so we
			add the annotation with the state 'Running'

			- if dState is equal to Running:
				* check if the namespace should be suspended, based on the `dailySuspendTime`` annotation. If it should:
					1. update dState to Suspended
					2. update the namespace annotation to Suspended

				* check if the namespace should be suspended, based on the `nextSuspendTime`` annotation. If it should:
					1. we do the same as for dailySuspendTime annotation

			- if dState is equal to Suspended, the switch-case will do nothing yet and go to the next step.
			- if dState ends in the default case, it means that the state has not been recognised, so
			we have to error
		*/

		stepName = "1/3 - define namespace state from annotation"
		sLogger.Debug().Str("step", stepName).Msg("starting step")

		dState := n.Annotations[eng.Options.Prefix+DesiredState]
		switch dState {
		case "":
			sLogger.Debug().Str("step", stepName).Msgf("namespace has no '%s' annotation, it is probably the first time I see it", eng.Options.Prefix+DesiredState)
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				sLogger.Trace().Int("step", 1).Msg("get namespace")
				res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				// we set the annotation to running
				sLogger.Trace().Str("step", stepName).Msgf("setting namespace annotation '%s=%s'", eng.Options.Prefix+DesiredState, Running)
				res.Annotations[eng.Options.Prefix+DesiredState] = Running

				sLogger.Trace().Str("step", stepName).Msg("updating namespace")
				_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
				return err
			}); err != nil {
				sLogger.Error().Err(err).Msg("cannot update namespace object")
				// we give up and handle the next namespace
				sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
				continue
			}
			sLogger.Debug().Str("step", stepName).Msgf("added annotation '%s=%s' to namespace", eng.Options.Prefix+DesiredState, Running)

			// we now update the value of dState to match the new namespace annotation
			sLogger.Debug().Str("step", stepName).Msgf("updating internal state to '%s'", Running)
			dState = Running
		case Running:
			sLogger.Debug().Str("step", stepName).Msgf("found annotation '%s=%s'", eng.Options.Prefix+DesiredState, dState)

			// check if dailySuspendTime is set and past
			sLogger.Debug().Str("step", stepName).Msgf("checking annotation '%s'", eng.Options.Prefix+DailySuspendTime)
			if val, ok := n.Annotations[eng.Options.Prefix+DailySuspendTime]; ok {
				sLogger.Info().Str("step", stepName).Msgf("found annotation '%s'='%s'", eng.Options.Prefix+DailySuspendTime, val)

				now, suspendAt, err := getTimes(val)
				if err != nil {
					sLogger.Warn().Err(err).Msgf("cannot parse '%s' annotation on namespace", eng.Options.Prefix+DailySuspendTime)
				}

				if err == nil && suspendAt <= now {
					sLogger.Debug().
						Str("step", stepName).
						Msgf("%s is less or equal to now (value: %d, now: %d), updating annotation '%s' to '%s'", eng.Options.Prefix+DailySuspendTime, suspendAt, now, eng.Options.Prefix+DesiredState, Suspended)

					// NOTICE: Seems same content than L51-L69
					if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						sLogger.Trace().Str("step", stepName).Msgf("get namespace")
						res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						// we set the annotation to suspended
						sLogger.Trace().Str("step", stepName).Msgf("setting namespace annotation '%s=%s'", eng.Options.Prefix+DesiredState, Suspended)
						res.Annotations[eng.Options.Prefix+DesiredState] = Suspended

						sLogger.Trace().Str("step", stepName).Msgf("updating namespace")
						_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
						return err
					}); err != nil {
						sLogger.Error().Err(err).Msgf("cannot update namespace object")
						// we give up and handle the next namespace
						sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
						continue
					} else {
						sLogger.Debug().Str("step", stepName).Msgf("added annotation '%s=%s' to namespace, going back to the start of the switch-case", DesiredState, Suspended)

						// we now update the value of dState to match the new namespace annotation
						dState = Suspended
						break
					}
				} else {
					sLogger.Debug().Str("step", stepName).Msgf("%s is not yet past (value: %d, now: %d), not doing anything", eng.Options.Prefix+DailySuspendTime, suspendAt, now)
				}
			} else {
				sLogger.Warn().Msgf("'%s' annotation not found on namespace", eng.Options.Prefix+DailySuspendTime)
			}

			// check if nextSuspendTime exists and is past
			sLogger.Debug().Str("step", stepName).Msgf("checking annotation '%s'", eng.Options.Prefix+NextSuspendTime)
			if val, ok := n.Annotations[eng.Options.Prefix+NextSuspendTime]; ok {
				sLogger.Info().Str("step", stepName).Msgf("found annotation '%s'='%s'", eng.Options.Prefix+NextSuspendTime, val)

				nextSuspendAt, err := time.Parse(time.RFC822Z, val)
				if err != nil {
					sLogger.Error().Err(err).Msgf("cannot parse '%s' value '%s' in time format '%s'", eng.Options.Prefix+NextSuspendTime, val, time.RFC822Z)
					continue
				}

				if time.Now().Local().After(nextSuspendAt) {
					sLogger.Debug().Str("step", stepName).
						Msgf("%s is past, updating annotation '%s' to '%s'", eng.Options.Prefix+NextSuspendTime, eng.Options.Prefix+DesiredState, Suspended)
					if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						sLogger.Trace().Str("step", stepName).Msgf("get namespace")
						res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						// we set the annotation to suspended
						sLogger.Trace().Str("step", stepName).Msgf("setting namespace annotation '%s=%s'", eng.Options.Prefix+DesiredState, Suspended)
						res.Annotations[eng.Options.Prefix+DesiredState] = Suspended

						sLogger.Trace().Str("step", stepName).Msgf("updating namespace")
						_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
						return err
					}); err != nil {
						sLogger.Error().Err(err).Msgf("cannot update namespace object")
						// we give up and handle the next namespace
						sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
						continue
					} else {
						sLogger.Debug().Str("step", stepName).Msgf("added annotation '%s=%s' to namespace, going back to the start of the switch-case", DesiredState, Suspended)

						// we now update the value of dState to match the new namespace annotation
						dState = Suspended
						break
					}
				} else {
					sLogger.Debug().Str("step", stepName).Msgf("%s is not yet past (value: %s, now: %s), not doing anything", NextSuspendTime+DailySuspendTime, nextSuspendAt, time.Now().Local())
				}
			} else {
				sLogger.Warn().Msgf("'%s' annotation not found on namespace", eng.Options.Prefix+NextSuspendTime)
			}
		case Suspended:
			sLogger.Debug().Str("step", stepName).Msgf("found annotation '%s=%s'", eng.Options.Prefix+DesiredState, dState)
		default:
			sLogger.Error().Err(errors.New("state not recognised: "+dState)).Msgf("state %s is not recognised", dState)
			// we give up and handle the next namespace
			sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
			continue
		}

		/*
			Step 2

			In order to be able to edit the resources, we first need to get all of them from
			the namespace.
		*/
		stepName = "2/3 - get namespace resources"
		// get deployments of the namespace
		sLogger.Debug().Str("step", stepName).Msg("getting namespace resources to manage")
		sLogger.Debug().Str("step", stepName).Str("resource", "deployments").Msg("get resource from k8s")
		deployments, err := cs.AppsV1().Deployments(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().Err(err).Msg("cannot list deployments")
		}

		// get cronjobs of the namespace
		// we need to support both batchv1 and batchv1beta
		sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Str("apiVersion", "bacthv1").Msg("get resource from k8s")
		cronjobs, err := cs.BatchV1().CronJobs(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Warn().Err(err).Msg("cannot list cronjobs with API version batchv1")
		}

		sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Str("apiVersion", "batchv1beta").Msg("get resource from k8s")
		cronjobsBeta, err := cs.BatchV1beta1().CronJobs(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Warn().Err(err).Msg("cannot list cronjobs with API version batchv1beta")
		}

		// get statefulsets of the namespace
		sLogger.Debug().Str("step", stepName).Str("resource", "statefulsets").Msg("get resource from k8s")
		statefulsets, err := cs.AppsV1().StatefulSets(n.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			sLogger.Fatal().Err(err).Msg("cannot list statefulsets")
		}

		scaledobjects := &kedav1alpha1.ScaledObjectList{}
		if eng.Options.KedaEnabled {
			sLogger.Debug().Str("step", stepName).Str("resource", "scaledobjects").Msg("get resource from k8s")
			// get scaledobjects of the namespace
			scaledobjects, err = kedacs.ScaledObjects(n.Name).List(ctx, metav1.ListOptions{})
			if err != nil {
				sLogger.Fatal().Err(err).Msg("cannot list scaledobjects")
			}
		}

		/*
			Step 3

			If we end up here, it means that:
			- the namespace has a desiredState annotation
			- the annotation is valid

			Now, we have to do another switch-case statement to manage the behavior of
			the underlying replicas.
			This switch-case will match dState again, with different behaviors:
			- if dState == Suspended:
				* be sure that the undelying resources are suspended. If not, downscale them

			- if dState == Running:
				* check if the namespace is correctly Running, as the annotation might have been set manually. If not,
				  upscale everything
		*/
		stepName = "3/3 - handle desiredState"
		sLogger.Debug().Str("step", stepName).Msgf("namespace is seen as being '%s'", dState)
		switch dState {
		case Suspended:
			sLogger.Debug().Str("step", stepName).Msg("checking suspended Conformity")
			// the checks will be done concurrently to optimise verification duration
			// var wg sync.WaitGroup
			// wg.Add(4)

			// check and patch deployments
			sLogger.Debug().Str("step", stepName).Str("resource", "deployments").Msg("checking suspended Conformity")
			// go func() {
			if err := checkSuspendedDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.Prefix); err != nil {
				sLogger.Error().Err(err).Str("object", "deployment").Msg("suspended conformity checks failed")
			}
			// wg.Done()
			// }()

			// check and patch cronjobs
			sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Msg("checking suspended Conformity")
			// go func() {
			if err := checkSuspendedCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name); err != nil {
				sLogger.Error().Err(err).Str("object", "cronjob").Msg("suspended cronjobs conformity checks failed")
			}
			// wg.Done()
			// }()

			sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Msg("checking suspended Conformity")
			// go func() {
			if err := checkSuspendedCronjobsBetaConformity(ctx, sLogger, cronjobsBeta.Items, cs, n.Name); err != nil {
				sLogger.Error().Err(err).Str("object", "cronjob").Msg("suspended cronjobs conformity checks failed")
			}
			// wg.Done()
			// }()

			// check and patch statefulsets
			sLogger.Debug().Str("step", stepName).Str("resource", "statefulsets").Msg("checking suspended Conformity")
			// go func() {
			if err := checkSuspendedStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name, eng.Options.Prefix); err != nil {
				sLogger.Error().Err(err).Str("object", "statefulset").Msg("suspended statefulsets conformity checks failed")
			}
			// wg.Done()
			// }()

			if eng.Options.KedaEnabled {
				// wg.Add(1)
				// check and patch scaledobjects
				sLogger.Debug().Str("step", stepName).Str("resource", "scaledobjects").Msg("checking suspended Conformity")
				// go func() {
				if err := checkSuspendedScaledObjectsConformity(ctx, sLogger, scaledobjects.Items, kedacs, n.Name); err != nil {
					sLogger.Error().Err(err).Str("object", "scaledobjects").Msg("suspended scaledobjects conformity checks failed")
				}
				// wg.Done()
				// }()
			}
			// we wait for all the checks to be done
			// wg.Wait()
			sLogger.Debug().Str("step", stepName).Msg("checking suspended Conformity done")

			// Cleaning-up annotations
			sLogger.Debug().Str("step", stepName).Msgf("checking annotation '%s'", eng.Options.Prefix+NextSuspendTime)
			if _, ok := n.Annotations[eng.Options.Prefix+NextSuspendTime]; ok {
				sLogger.Debug().Str("step", stepName).Msgf("found annotation '%s', cleanning-up", eng.Options.Prefix+NextSuspendTime)
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					sLogger.Trace().Str("step", stepName).Msgf("get namespace")
					res, err := cs.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					sLogger.Trace().Str("step", stepName).Msgf("removing namespace annotation '%s'", eng.Options.Prefix+NextSuspendTime)
					delete(res.Annotations, eng.Options.Prefix+NextSuspendTime)

					sLogger.Trace().Str("step", stepName).Msgf("updating namespace")
					_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().Err(err).Msgf("cannot update namespace object")
					// we give up and handle the next namespace
					sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
					continue
				} else {
					sLogger.Debug().Str("step", stepName).Msgf("removed annotation '%s'", eng.Options.Prefix+NextSuspendTime)
				}
			} else {
				sLogger.Debug().Str("step", stepName).Msgf("annotation '%s' not found, nothing to do", eng.Options.Prefix+NextSuspendTime)
			}

		case Running:
			// var wg sync.WaitGroup
			var patchedResourcesCounter int

			sLogger.Debug().Str("step", stepName).Msgf("namespace is seen as being '%s'", dState)
			// wg.Add(4)

			sLogger.Debug().Str("step", stepName).Msg("checking running conformity")

			// check and patch deployments
			sLogger.Debug().Str("step", stepName).Str("resource", "deployments").Msg("checking running conformity")
			// go func() {
			hasBeenPatched, err := checkRunningDeploymentsConformity(ctx, sLogger, deployments.Items, cs, n.Name, eng.Options.Prefix)
			if err != nil {
				sLogger.Error().Err(err).Msg("running deployments conformity checks failed")
			}
			if hasBeenPatched {
				sLogger.Debug().Str("step", stepName).Str("resource", "deployments").Msg("resource has been patched")
				patchedResourcesCounter++
			}
			// wg.Done()
			// }()

			// check and patch cronjobs
			sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Msg("checking running conformity")
			// go func() {
			hasBeenPatched, err = checkRunningCronjobsConformity(ctx, sLogger, cronjobs.Items, cs, n.Name)
			if err != nil {
				sLogger.Error().Err(err).Msg("running cronjobs conformity checks failed")
			}
			if hasBeenPatched {
				sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Msg("resource has been patched")
				patchedResourcesCounter++
			}
			// wg.Done()
			// }()

			sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Msg("checking running conformity")
			// go func() {
			hasBeenPatched, err = checkRunningCronjobsBetaConformity(ctx, sLogger, cronjobsBeta.Items, cs, n.Name)
			if err != nil {
				sLogger.Error().Err(err).Msg("running cronjobs conformity checks failed")
			}
			if hasBeenPatched {
				sLogger.Debug().Str("step", stepName).Str("resource", "cronjobs").Msg("resource has been patched")
				patchedResourcesCounter++
			}
			// wg.Done()
			// }()

			// check and patch statefulsets
			sLogger.Debug().Str("step", stepName).Str("resource", "statefulsets").Msg("checking running conformity")
			// go func() {
			hasBeenPatched, err = checkRunningStatefulsetsConformity(ctx, sLogger, statefulsets.Items, cs, n.Name, eng.Options.Prefix)
			if err != nil {
				sLogger.Error().Err(err).Msg("running statefulsets conformity checks failed")
			}
			if hasBeenPatched {
				sLogger.Debug().Str("step", stepName).Str("resource", "statefulsets").Msg("resource has been patched")
				patchedResourcesCounter++
			}
			// wg.Done()
			// }()

			if eng.Options.KedaEnabled {
				// wg.Add(1)
				// check and patch scaledobjects
				sLogger.Debug().Str("step", stepName).Str("resource", "scaledobjects").Msg("checking running conformity")
				// go func() {
				hasBeenPatched, err := checkRunningScaledObjectsConformity(ctx, sLogger, scaledobjects.Items, kedacs, n.Name)
				if err != nil {
					sLogger.Error().Err(err).Msg("running scaledobjects conformity checks failed")
				}
				if hasBeenPatched {
					sLogger.Debug().Str("step", stepName).Str("resource", "scaledobjects").Msg("resource has been patched")
					patchedResourcesCounter++
				}
				// wg.Done()
				// }()
			}

			// we wait for all the checks to be done
			// wg.Wait()
			sLogger.Debug().Str("step", stepName).Msg("checking running conformity done")

			// now we can check if patchedResourcesCounter is > 0 and add nextSuspendTime depending of the result
			if patchedResourcesCounter > 0 {
				sLogger.Debug().Str("step", stepName).Msg("namespace has been unsuspended manually")

				sLogger.Debug().Str("step", stepName).Msgf("checking annotation '%s'", eng.Options.Prefix+NextSuspendTime)
				if val, ok := n.Annotations[eng.Options.Prefix+NextSuspendTime]; ok {
					sLogger.Info().Str("step", stepName).Msgf("found annotation '%s'='%s', not updating it", eng.Options.Prefix+NextSuspendTime, val)
					sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
					continue
				}

				sLogger.Info().Msgf("'%s' annotation not found on namespace", eng.Options.Prefix+NextSuspendTime)
				sLogger.Info().Msgf("adding the annotation '%s' to namespace (engine configured duration: '%s'", eng.Options.Prefix+NextSuspendTime, eng.RunningDuration)
				if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					sLogger.Trace().Str("step", stepName).Msg("get namespace")
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
					nextSuspendTimeValue := time.Now().Local().Add(eng.RunningDuration).Format(time.RFC822Z)
					sLogger.Trace().Str("step", stepName).Msgf("setting namespace annotation '%s=%s'", eng.Options.Prefix+NextSuspendTime, nextSuspendTimeValue)
					res.Annotations[eng.Options.Prefix+NextSuspendTime] = nextSuspendTimeValue

					sLogger.Trace().Str("step", stepName).Msg("update namespace")
					_, err = cs.CoreV1().Namespaces().Update(ctx, res, metav1.UpdateOptions{})
					return err
				}); err != nil {
					sLogger.Error().Err(err).Msgf("cannot add annotation '%s' to namespace", eng.Options.Prefix+NextSuspendTime)
				}
			}
		}

		sLogger.Debug().Str("step", stepName).Msgf("suspender loop ended, duration: %s", time.Since(start))
	}
}
