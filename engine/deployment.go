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
func checkRunningDeploymentsConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns string, dr bool) error {
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

			l.Info().
				Str("namespace", ns).
				Str("deployment", d.Name).
				Msgf("scaling %s from 0 to %d replicas", d.Name, repl)
			// patch the deployment
			if !dr {
				if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, repl); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkSuspendedDeploymentsConformity(ctx context.Context, l zerolog.Logger, deployments []appsv1.Deployment, cs *kubernetes.Clientset, ns string, dr bool) error {
	for _, d := range deployments {
		repl := int(*d.Spec.Replicas)
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Info().
				Str("namespace", ns).
				Str("deployment", d.Name).
				Msgf("scaling %s from %d to 0 replicas", d.Name, repl)
			// patch the deployment if -dryrun is not set
			if !dr {
				if err := patchDeploymentReplicas(ctx, cs, ns, d.Name, 0); err != nil {
					return err
				}
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
