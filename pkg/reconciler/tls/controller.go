package tls

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kuadrant/kcp-glbc/pkg/reconciler"
)

const (
	controllerName      = "kcp-glbc-secrets"
	tlsIssuerAnnotation = "kuadrant.dev/tls-issuer"
)

type ControllerConfig struct {
	GlbcKubeClient     kubernetes.Interface
	GlbcSecretInformer corev1informers.SecretInformer
	KcpKubeClient      kubernetes.ClusterInterface
}

type Controller struct {
	*reconciler.Controller
	glbcKubeClient     kubernetes.Interface
	glbcSecretInformer corev1informers.SecretInformer
	kcpKubeClient      kubernetes.ClusterInterface
}

func NewController(config *ControllerConfig) (*Controller, error) {
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName)
	c := &Controller{
		Controller:         reconciler.NewController(controllerName, queue),
		glbcKubeClient:     config.GlbcKubeClient,
		kcpKubeClient:      config.KcpKubeClient,
		glbcSecretInformer: config.GlbcSecretInformer,
	}
	c.Process = c.process

	c.glbcSecretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret := obj.(*corev1.Secret)
			issuer, hasIssuer := secret.Annotations[tlsIssuerAnnotation]
			if hasIssuer {
				tlsCertificateSecretCount.WithLabelValues(issuer).Inc()
			}
			c.Enqueue(obj)
		},
		UpdateFunc: func(old, obj interface{}) {
			if old.(*corev1.Secret).ResourceVersion != obj.(*corev1.Secret).ResourceVersion {
				c.Enqueue(obj)
			}
		},
		DeleteFunc: func(obj interface{}) {
			secret := obj.(*corev1.Secret)
			issuer, hasIssuer := secret.Annotations[tlsIssuerAnnotation]
			if hasIssuer {
				tlsCertificateSecretCount.WithLabelValues(issuer).Dec()
			}
			c.Enqueue(obj)
		},
	})

	return c, nil
}

func (c *Controller) process(ctx context.Context, key string) error {
	secret, exists, err := c.glbcSecretInformer.Informer().GetIndexer().GetByKey(key)
	if err != nil {
		return err
	}

	if !exists {
		c.Logger.Info("Secret was deleted", "key", key)
		return nil
	}

	current := secret.(*corev1.Secret)
	previous := current.DeepCopy()
	if err = c.reconcile(ctx, current); err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(previous, current) {
		_, err := c.glbcKubeClient.CoreV1().Secrets(current.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	}

	return nil
}
