package service

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

func (c *Controller) reconcile(ctx context.Context, service *corev1.Service) error {
	return nil
}
