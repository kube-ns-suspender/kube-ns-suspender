package engine

import (
	"context"
	"strconv"

	monitoring "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promClientset "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func checkRunningPrometheusesConformity(ctx context.Context, l zerolog.Logger, prometheuses []*monitoring.Prometheus, cs *promClientset.Clientset, ns, prefix string) (bool, error) {
	hasBeenPatched := false
	var desiredReplicas int
	var err error
	for _, p := range prometheuses {
		repl := *p.Spec.Replicas
		// if the current number of replicas is 0, we want to evaluate whether to scale back up to the original replicas
		if repl == 0 {
			// if no originalReplicas annotation is found, we assume the desired replicas is 0 to handle a statefulset scaled to 0
			desiredReplicas = 0
			// get the desired number of replicas
			if p.Annotations != nil {
				if val, ok := p.Annotations[prefix+originalReplicas]; ok {
					desiredReplicas, err = strconv.Atoi(val)
					if err != nil {
						return hasBeenPatched, err
					}
				}
			}
			if desiredReplicas != 0 {
				l.Info().Str("statefulset", p.Name).Msgf("scaling %s from 0 to %d replicas", p.Name, desiredReplicas)
				// patch the prometheus
				//_, err := cs.MonitoringV1().Prometheuses(ns).Update(context.TODO(), p, metav1.UpdateOptions{})
				if err := patchPrometheusReplicas(ctx, cs, ns, p.Name, prefix, 0); err != nil {
					return hasBeenPatched, err
				}

				hasBeenPatched = true
			}
		}
	}
	return hasBeenPatched, nil
}

func checkSuspendedPrometheusesConformity(ctx context.Context, l zerolog.Logger, prometheuses []*monitoring.Prometheus, cs *promClientset.Clientset, ns, prefix string) error {
	for _, p := range prometheuses {
		repl := *p.Spec.Replicas
		if repl != 0 {
			// TODO: what about fixing the annotation original Replicas here ?
			l.Info().Str("prometheus", p.Name).Msgf("scaling %s from %d to 0 replicas", p.Name, repl)
			// patch the deployment
			if err := patchPrometheusReplicas(ctx, cs, ns, p.Name, prefix, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func patchPrometheusReplicas(ctx context.Context, cs *promClientset.Clientset, ns, p, prefix string, repl int) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.MonitoringV1().Prometheuses(ns).Get(ctx, p, metav1.GetOptions{})
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
		_, err = cs.MonitoringV1().Prometheuses(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
