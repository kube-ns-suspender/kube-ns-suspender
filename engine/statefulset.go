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

func checkRunningStatefulsetsConformity(ctx context.Context, l zerolog.Logger, statefulsets []appsv1.StatefulSet, cs *kubernetes.Clientset, ns, prefix string) (bool, error) {
	hasBeenPatched := false
	for _, ss := range statefulsets {
		repl := int(*ss.Spec.Replicas)
		// if the current number of replicas is 0, we want to evaluate whether to scale back up to the original replicas
		if repl == 0 {
			// if no originalReplicas annotation is found, we assume the desired replicas is 0 to handle a statefulset scaled to 0
			desiredRepl := 0
			var err error
			// get the desired number of replicas
			if ss.Annotations != nil {
				if val, ok := ss.Annotations[prefix+originalReplicas]; ok {
					desiredRepl, err = strconv.Atoi(val)
					if err != nil {
						return hasBeenPatched, err
					}
				}
			}
			if desiredRepl != 0 {
				l.Info().Str("statefulset", ss.Name).Msgf("scaling %s from 0 to %d replicas", ss.Name, desiredRepl)
				// patch the statefulset
				if err := patchStatefulsetReplicas(ctx, cs, ns, ss.Name, prefix, desiredRepl); err != nil {
					return hasBeenPatched, err
				}
				hasBeenPatched = true
			}
		}
	}
	return hasBeenPatched, nil
}

func checkSuspendedStatefulsetsConformity(ctx context.Context, l zerolog.Logger, statefulsets []appsv1.StatefulSet, cs *kubernetes.Clientset, ns, prefix string) error {
	for _, ss := range statefulsets {
		repl := int(*ss.Spec.Replicas)
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Info().Str("statefulset", ss.Name).Msgf("scaling %s from %d to 0 replicas", ss.Name, repl)
			// patch the deployment
			if err := patchStatefulsetReplicas(ctx, cs, ns, ss.Name, prefix, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchStatefulsetSuspend updates the number of replicas of a given statefulset
func patchStatefulsetReplicas(ctx context.Context, cs *kubernetes.Clientset, ns, ss, prefix string, repl int) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.AppsV1().StatefulSets(ns).Get(ctx, ss, metav1.GetOptions{})
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
			// statefulset is allowed to scale back to 0
			delete(result.Annotations, prefix+originalReplicas)
		}
		result.Spec.Replicas = flip(int32(repl))
		_, err = cs.AppsV1().StatefulSets(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
