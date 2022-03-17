//go:build e2e
// +build e2e

package support

import (
	"context"
	"sync"
	"testing"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
)

type Test interface {
	T() *testing.T
	Ctx() context.Context
	Client() Client

	gomega.Gomega

	NewTestWorkspace() *tenancyv1alpha1.Workspace
	NewTestNamespace(...Option) *corev1.Namespace
	NewWorkloadCluster(name string, options ...Option) *clusterv1alpha1.Cluster
}

type Option interface {
	applyTo(metav1.Object) error
}

func With(t *testing.T) Test {
	ctx := context.Background()
	if deadline, ok := t.Deadline(); ok {
		withDeadline, cancel := context.WithDeadline(ctx, deadline)
		t.Cleanup(cancel)
		ctx = withDeadline
	}

	return &T{
		WithT: gomega.NewWithT(t),
		t:     t,
		ctx:   ctx,
	}
}

type T struct {
	*gomega.WithT
	t      *testing.T
	ctx    context.Context
	client Client
	once   sync.Once
}

func (t *T) T() *testing.T {
	return t.t
}

func (t *T) Ctx() context.Context {
	return t.ctx
}

func (t *T) Client() Client {
	t.once.Do(func() {
		c, err := newTestClient()
		if err != nil {
			t.T().Fatalf("Error creating client: %v", err)
		}
		t.client = c
	})
	return t.client
}

func (t *T) NewTestWorkspace() *tenancyv1alpha1.Workspace {
	workspace := createTestWorkspace(t)
	t.T().Cleanup(func() {
		deleteTestWorkspace(t, workspace)
	})
	t.Eventually(Workspace(t, workspace.Name)).Should(gomega.WithTransform(
		ConditionStatus(tenancyv1alpha1.WorkspaceScheduled),
		gomega.Equal(corev1.ConditionTrue),
	))
	return workspace
}

func (t *T) NewTestNamespace(options ...Option) *corev1.Namespace {
	namespace := createTestNamespace(t, options...)
	t.T().Cleanup(func() {
		deleteTestNamespace(t, namespace)
	})
	return namespace
}

func (t *T) NewWorkloadCluster(name string, options ...Option) *clusterv1alpha1.Cluster {
	return newWorkloadCluster(t, name, options...)
}
