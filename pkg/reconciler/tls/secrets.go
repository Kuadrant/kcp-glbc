package tls

import (
	"context"

	v1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	"github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

const (
	secretsFinalizer   = "kcp.dev/cascade-cleanup"
	tlsReadyAnnotation = "kuadrant.dev/tls.enabled"
)

// this controller watches the control cluster and mirrors cert secrets into the KCP cluster
func (c *Controller) reconcile(ctx context.Context, secret *v1.Secret) error {
	// create our context to avoid repeatedly pulling out annotations etc
	kcpCtx, err := cluster.NewKCPObjectMapper(secret)
	if err != nil {
		// TODO: use label selector in the controller to filter Secrets out
		if cluster.IsNoContextErr(err) {
			// ignore this secret
			return nil
		}
		return err
	}

	if secret.DeletionTimestamp != nil {
		c.Logger.Info("Removing mirrored Secret from KCP, control cluster Secret deleted", "secret", secret)
		if err := c.ensureDelete(ctx, kcpCtx, secret); err != nil {
			return err
		}
		// remove finalizer from the Secret, so it can be cleaned up
		removeFinalizer(secret, secretsFinalizer)
		if _, err = c.glbcKubeClient.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil && !k8errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	if !containsFinalizer(secret, secretsFinalizer) {
		addFinalizer(secret, secretsFinalizer)
		secret, err = c.glbcKubeClient.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	if err := c.ensureMirrored(ctx, kcpCtx, secret); err != nil {
		c.Logger.Error(err, "Failed to mirror Secret", "secret", secret)
		return err
	}

	return nil
}

func containsFinalizer(secret *v1.Secret, finalizer string) bool {
	for _, f := range secret.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func removeFinalizer(secret *v1.Secret, finalizer string) {
	for i, v := range secret.Finalizers {
		if v == finalizer {
			secret.Finalizers[i] = secret.Finalizers[len(secret.Finalizers)-1]
			secret.Finalizers = secret.Finalizers[:len(secret.Finalizers)-1]
			return
		}
	}
}

func addFinalizer(secret *v1.Secret, finalizer string) {
	for _, v := range secret.Finalizers {
		if v == finalizer {
			return
		}
	}
	secret.Finalizers = append(secret.Finalizers, finalizer)
}

func (c *Controller) ensureDelete(ctx context.Context, kctx cluster.ObjectMapper, secret *v1.Secret) error {
	if err := c.kcpKubeClient.Cluster(logicalcluster.New(kctx.Workspace())).CoreV1().Secrets(kctx.Namespace()).Delete(ctx, kctx.Name(), metav1.DeleteOptions{}); err != nil && !k8errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (c *Controller) ensureMirrored(ctx context.Context, kctx cluster.ObjectMapper, secret *v1.Secret) error {
	c.Logger.Info("Mirroring TLS secret", "name", kctx.Name(), "workspace", kctx.Workspace(), "namespace", kctx.Namespace())
	mirror := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kctx.Name(),
			Namespace: kctx.Namespace(),
			Labels:    kctx.Labels(),
		},
		Data: secret.Data,
		Type: secret.Type,
	}
	// using the KCP client here to target the KCP cluster
	secretClient := c.kcpKubeClient.Cluster(logicalcluster.New(kctx.Workspace())).CoreV1().Secrets(kctx.Namespace())
	mirrored, err := secretClient.Create(ctx, mirror, metav1.CreateOptions{})
	if err != nil {
		if !k8errors.IsAlreadyExists(err) {
			return err
		}
		s, err := secretClient.Get(ctx, mirror.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		mirror.ResourceVersion = s.ResourceVersion
		mirror.UID = s.UID
		if _, err := secretClient.Update(ctx, mirror, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	// find the Ingress this Secret is for and add an annotation to notify TLS certificate is ready and trigger reconcile
	ingressClient := c.kcpKubeClient.Cluster(logicalcluster.New(kctx.Workspace())).NetworkingV1().Ingresses(kctx.Namespace())
	rootIngress, err := ingressClient.Get(ctx, kctx.OwnedBy(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if rootIngress.Annotations == nil {
		rootIngress.Annotations = map[string]string{}
	}
	if _, ok := rootIngress.Annotations[tlsReadyAnnotation]; !ok {
		rootIngress.Annotations[tlsReadyAnnotation] = "true"
		if _, err := ingressClient.Update(ctx, rootIngress, metav1.UpdateOptions{}); err != nil {
			return err
		}
		c.observeCertificateIssuanceDuration(kctx, mirrored.CreationTimestamp, secret.Annotations[tlsIssuerAnnotation])
	}

	return nil
}

func (c *Controller) observeCertificateIssuanceDuration(kctx cluster.ObjectMapper, creationTimestamp metav1.Time, issuer string) {
	// FIXME: refactor the certificate management so that metrics reflect actual state transitions rather than client requests, and so that it's possible to observe issuance errors
	// The certificate request has successfully completed
	tlsCertificateRequestTotal.WithLabelValues(issuer, resultLabelSucceeded).Inc()
	// The certificate request has successfully completed so there is one less pending request
	tls.CertificateRequestCount.WithLabelValues(issuer).Dec()

	tlsCertificateIssuanceDuration.
		WithLabelValues(issuer, resultLabelSucceeded).
		Observe(creationTimestamp.Sub(kctx.CreationTimestamp().Time).Seconds())
}
