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

func checkRunningStatefulsetsConformity(ctx context.Context, l zerolog.Logger, statefulsets []appsv1.StatefulSet, cs *kubernetes.Clientset, ns string, dr bool) error {
	for _, ss := range statefulsets {
		repl := int(*ss.Spec.Replicas)
		if repl == 0 {
			// get the desired number of replicas
			repl, err := strconv.Atoi(ss.Annotations["kube-ns-suspender/originalReplicas"])
			if err != nil {
				return err
			}

			l.Info().
				Str("statefulset", ss.Name).
				Msgf("scaling %s from 0 to %d replicas", ss.Name, repl)
			// patch the statefulset
			if err := patchStatefulsetReplicas(ctx, cs, ns, ss.Name, repl); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSuspendedStatefulsetsConformity(ctx context.Context, l zerolog.Logger, statefulsets []appsv1.StatefulSet, cs *kubernetes.Clientset, ns string, dr bool) error {
	for _, ss := range statefulsets {
		repl := int(*ss.Spec.Replicas)
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Info().
				Str("statefulset", ss.Name).
				Msgf("scaling %s from %d to 0 replicas", ss.Name, repl)
			// patch the deployment
			if err := patchStatefulsetReplicas(ctx, cs, ns, ss.Name, 0); err != nil {
				return err
			}
		}
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
		// if we want 0 replicas, it means that we are suspending the namespace,
		// so before adjusting the replicas count, we want to save it for later
		if repl == 0 {
			result.Annotations["kube-ns-suspender/originalReplicas"] = strconv.Itoa(int(*result.Spec.Replicas))
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
