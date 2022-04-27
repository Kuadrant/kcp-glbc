package tls

import (
	"context"

	v1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

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
	// TODO: use label selector in the controller to filter Secrets out
	if err != nil && cluster.IsNoContextErr(err) {
		// ignore this secret
		return nil
	}
	if err != nil {
		return err
	}

	if secret.DeletionTimestamp != nil {
		klog.Infof("control cluster secret %s deleted removing mirrored secret from kcp", secret.Name)
		if err := c.ensureDelete(ctx, kcpCtx, secret); err != nil {
			return err
		}
		// remove finalizer from the control cluster secret so it can be cleaned up
		removeFinalizer(secret, secretsFinalizer)
		if _, err = c.glbcKubeClient.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil && !k8errors.IsNotFound(err) {
			return err
		}
		return nil
	}
	AddFinalizer(secret, secretsFinalizer)
	secret, err = c.glbcKubeClient.CoreV1().Secrets(secret.Namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	if err := c.ensureMirrored(ctx, kcpCtx, secret); err != nil {
		klog.Errorf("failed to mirror secret %s", err.Error())
		return err
	}

	return nil
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

func AddFinalizer(secret *v1.Secret, finalizer string) {
	for _, v := range secret.Finalizers {
		if v == finalizer {
			return
		}
	}
	secret.Finalizers = append(secret.Finalizers, finalizer)
}

func (c *Controller) ensureDelete(ctx context.Context, kctx cluster.ObjectMapper, secret *v1.Secret) error {
	if err := c.kcpClient.Cluster(logicalcluster.New(kctx.Workspace())).CoreV1().Secrets(kctx.Namespace()).Delete(ctx, kctx.Name(), metav1.DeleteOptions{}); err != nil && !k8errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (c *Controller) ensureMirrored(ctx context.Context, kctx cluster.ObjectMapper, secret *v1.Secret) error {
	klog.Infof("mirroring %s tls secret to workspace %s namespace %s and secret %s ", kctx.Name(), kctx.Workspace(), kctx.Namespace(), kctx.Name())
	mirror := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kctx.Name(),
			Namespace: kctx.Namespace(),
			Labels:    kctx.Labels(),
		},
		Data: secret.Data,
		Type: secret.Type,
	}
	secretClient := c.kcpClient.Cluster(logicalcluster.New(kctx.Workspace())).CoreV1().Secrets(kctx.Namespace())
	// using kcpClient here to target the KCP cluster
	_, err := secretClient.Create(ctx, mirror, metav1.CreateOptions{})
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
	ingressClient := c.kcpClient.Cluster(logicalcluster.New(kctx.Workspace())).NetworkingV1().Ingresses(kctx.Namespace())
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
		c.observeCertificateIssuanceDuration(kctx, secret)
	}

	return nil
}

func (c *Controller) observeCertificateIssuanceDuration(kctx cluster.ObjectMapper, secret *v1.Secret) {
	// FIXME: refactor the certificate management so that metrics reflect actual state transitions rather than client requests, and so that it's possible to observe issuance errors
	issuer := secret.Annotations[tlsIssuerAnnotation]
	hostname := kctx.Host()
	// The certificate request has successfully completed
	tlsCertificateRequestTotal.WithLabelValues(issuer, hostname, resultLabelSucceeded).Inc()
	// The certificate request has successfully completed so there is one less pending request
	tls.CertificateRequestCount.WithLabelValues(issuer, hostname).Dec()

	tlsCertificateIssuanceDuration.
		WithLabelValues(issuer, hostname, resultLabelSucceeded).
		Observe(secret.CreationTimestamp.Sub(kctx.CreationTimestamp().Time).Seconds())
}
