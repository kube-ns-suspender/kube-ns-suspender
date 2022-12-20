package engine

import (
	"context"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
	"github.com/rs/zerolog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const pauseAnnotation string = "autoscaling.keda.sh/paused-replicas"

func checkRunningScaledObjectsConformity(ctx context.Context, l zerolog.Logger, scaledobjects []kedav1alpha1.ScaledObject, cs *v1alpha1.KedaV1alpha1Client, ns string) (bool, error) {
	hasBeenPatched := false
	for _, c := range scaledobjects {
		l.Debug().Str("scaledobject", c.Name).Msgf("running with annotations %v", c.Annotations)
		if c.Annotations != nil {
			l.Debug().Str("scaledobject", c.Name).Msgf("found annotation %v", c.Annotations[pauseAnnotation])
			if _, ok := c.Annotations[pauseAnnotation]; ok {
				l.Info().Str("scaledobject", c.Name).Msgf("updating %s from paused to unpaused", c.Name)
				if err := patchScaledObjectSuspend(ctx, cs, ns, c.Name, false, l); err != nil {
					return hasBeenPatched, err
				}
				hasBeenPatched = true
			}
		}
	}
	return hasBeenPatched, nil
}

func checkSuspendedScaledObjectsConformity(ctx context.Context, l zerolog.Logger, scaledobjects []kedav1alpha1.ScaledObject, cs *v1alpha1.KedaV1alpha1Client, ns string) error {
	for _, c := range scaledobjects {
		l.Debug().Str("scaledobject", c.Name).Msgf("suspended with annotations %v", c.Annotations)
		if c.Annotations != nil {
			l.Debug().Str("scaledobject", c.Name).Msgf("found annotation %v", c.Annotations[pauseAnnotation])
		}
		paused := false
		if c.Annotations != nil {
			if _, ok := c.Annotations[pauseAnnotation]; ok {
				paused = true
			}
		}
		l.Debug().Str("scaledobject", c.Name).Msgf("paused is %v", paused)
		if !paused {
			l.Info().Str("scaledobject", c.Name).Msgf("updating %s from unpaused to paused", c.Name)
			if err := patchScaledObjectSuspend(ctx, cs, ns, c.Name, true, l); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchScaledObjectSuspend updates the suspend state of a given scaledobject
func patchScaledObjectSuspend(ctx context.Context, cs *v1alpha1.KedaV1alpha1Client, ns, c string, suspend bool, l zerolog.Logger) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.ScaledObjects(ns).Get(ctx, c, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if suspend {
			// there is no annotations in the object's manifest, so the map must
			// be initialized
			// see issue #85
			if result.Annotations == nil {
				result.Annotations = make(map[string]string)
			}
			result.Annotations[pauseAnnotation] = "0"
			l.Debug().Str("scaledobject", c).Msg("adding pause annotation")
		} else {
			if result.Annotations != nil {
				delete(result.Annotations, pauseAnnotation)
				l.Debug().Str("scaledobject", c).Msg("deleting pause annotation")
			}
		}
		_, err = cs.ScaledObjects(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
