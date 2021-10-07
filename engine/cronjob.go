package engine

import (
	"context"

	"github.com/rs/zerolog"
	"k8s.io/api/batch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

func checkRunningCronjobsConformity(ctx context.Context, l zerolog.Logger, cronjobs []v1beta1.CronJob, cs *kubernetes.Clientset, ns string) error {
	for _, c := range cronjobs {
		if *c.Spec.Suspend {
			l.Info().
				Str("namespace", ns).
				Str("cronjob", c.Name).
				Msgf("updating %s from suspend: true to suspend: false", c.Name)
			if err := patchCronjobSuspend(ctx, cs, ns, c.Name, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkSuspendedCronjobsConformity(ctx context.Context, l zerolog.Logger, cronjobs []v1beta1.CronJob, cs *kubernetes.Clientset, ns string) error {
	for _, c := range cronjobs {
		if !*c.Spec.Suspend {
			l.Info().
				Str("namespace", ns).
				Str("cronjob", c.Name).
				Msgf("updating %s from suspend: false to suspend: true", c.Name)
			if err := patchCronjobSuspend(ctx, cs, ns, c.Name, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchCronjobSuspend updates the suspend state of a giver cronjob
func patchCronjobSuspend(ctx context.Context, cs *kubernetes.Clientset, ns, c string, suspend bool) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		result, err := cs.BatchV1beta1().CronJobs(ns).Get(ctx, c, metav1.GetOptions{})
		if err != nil {
			return err
		}
		result.Spec.Suspend = &suspend
		_, err = cs.BatchV1beta1().CronJobs(ns).Update(ctx, result, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}
	return nil
}
