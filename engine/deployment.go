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
		if repl == 0 {
			// get the desired number of replicas
			repl, err := strconv.Atoi(d.Annotations[prefix+originalReplicas])
			if err != nil {
				return hasBeenPatched, err
			}

			l.Info().Str("deployment", d.Name).Msgf("scaling %s from 0 to %d replicas", d.Name, repl)
			// patch the deployment
			if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, prefix, repl); err != nil {
				return hasBeenPatched, err
			}
			hasBeenPatched = true
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
			result.Annotations[prefix+originalReplicas] = strconv.Itoa(int(*result.Spec.Replicas))
		}
		result.Spec.Replicas = flip(int32(repl))
		_, err = cs.AppsV1().Deployments(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
