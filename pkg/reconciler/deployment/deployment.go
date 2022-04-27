package deployment

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
)

func (c *Controller) reconcile(ctx context.Context, deployment *appsv1.Deployment) error {
	return nil
}
