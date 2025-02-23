package engine

import (
	"context"

	"github.com/rs/zerolog"
	v1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

func checkRunningJobsConformity(ctx context.Context, l zerolog.Logger, jobs []v1.Job, cs *kubernetes.Clientset, ns string) (bool, error) {
	hasBeenPatched := false
	for _, c := range jobs {
		if *c.Spec.Suspend {
			l.Info().Str("job", c.Name).Msgf("updating %s from suspend: true to suspend: false", c.Name)
			if err := patchJobSuspend(ctx, cs, ns, c.Name, false); err != nil {
				return hasBeenPatched, err
			}
			hasBeenPatched = true
		}
	}
	return hasBeenPatched, nil
}

func checkSuspendedJobsConformity(ctx context.Context, l zerolog.Logger, jobs []v1.Job, cs *kubernetes.Clientset, ns string) error {
	for _, c := range jobs {
		if !*c.Spec.Suspend {
			l.Info().Str("job", c.Name).Msgf("updating %s from suspend: false to suspend: true", c.Name)
			if err := patchJobSuspend(ctx, cs, ns, c.Name, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchJobSuspend updates the suspend state of a giver job
func patchJobSuspend(ctx context.Context, cs *kubernetes.Clientset, ns, c string, suspend bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.BatchV1().Jobs(ns).Get(ctx, c, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Suspend = &suspend
		_, err = cs.BatchV1().Jobs(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
}
