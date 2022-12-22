package engine

import (
	"context"
	"strconv"

	"github.com/rs/zerolog"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// checkRunningDeploymentsConformity verifies that all deployments within the namespace are
// currently running
func checkRunningDeploymentsConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns, prefix string) (bool, error) {
	hasBeenPatched := false
	for _, d := range deployments {
		repl := int(*d.Spec.Replicas)
		// if the current number of replicas is 0, we want to evaluate whether to scale back up to the original replicas
		if repl == 0 {
			// if no originalReplicas annotation is found, we assume the desired replicas is 0 to handle a deployment scaled to 0
			desiredRepl := 0
			var err error
			// get the desired number of replicas
			if d.Annotations != nil {
				if val, ok := d.Annotations[prefix+originalReplicas]; ok {
					desiredRepl, err = strconv.Atoi(val)
					if err != nil {
						return hasBeenPatched, err
					}
				}
			}
			if desiredRepl != 0 {
				l.Info().Str("deployment", d.Name).Msgf("scaling %s from 0 to %d replicas", d.Name, desiredRepl)
				// patch the deployment
				if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, prefix, desiredRepl); err != nil {
					return hasBeenPatched, err
				}
				hasBeenPatched = true
			}
		}
	}
	return hasBeenPatched, nil
}

func checkSuspendedDeploymentsConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns, prefix string) error {
	for _, d := range deployments {
		repl := int(*d.Spec.Replicas)
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Info().Str("deployment", d.Name).Msgf("scaling %s from %d to 0 replicas", d.Name, repl)
			// patch the deployment
			if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, prefix, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchDeploymentReplicas updates the number of replicas of a given deployment
func patchDeploymentReplicas(ctx context.Context, cs *kubernetes.Clientset, ns, d, prefix string, repl int) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.AppsV1().Deployments(ns).Get(ctx, d, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// if we want 0 replicas, it means that we are suspending the namespace,
		// so before adjusting the replicas count, we want to save it for later
		if repl == 0 {
			// there is no annotations in the object's manifest, so the map must
			// be initialized
			// see issue #85
			if result.Annotations == nil {
				result.Annotations = make(map[string]string)
			}
			result.Annotations[prefix+originalReplicas] = strconv.Itoa(int(*result.Spec.Replicas))
		} else {
			// we are unsuspending the namespace, so clear the originalReplicas so that the
			// deployment is allowed to scale back to 0
			delete(result.Annotations, prefix+originalReplicas)
		}
		result.Spec.Replicas = flip(int32(repl))
		_, err = cs.AppsV1().Deployments(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
