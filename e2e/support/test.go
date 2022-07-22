//go:build e2e

/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package support

import (
	"context"
	"github.com/kcp-dev/kcp/pkg/apis/tenancy/v1beta1"
	"sync"
	"testing"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	workloadv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/workload/v1alpha1"
)

type Test interface {
	T() *testing.T
	Ctx() context.Context
	Client() Client

	gomega.Gomega

	NewTestWorkspace() *v1beta1.Workspace
	NewAPIBinding(name string, options ...Option) *apisv1alpha1.APIBinding
	NewTestNamespace(...Option) *corev1.Namespace
	NewSyncTarget(name string, options ...Option) *workloadv1alpha1.SyncTarget
}

type Option interface {
	applyTo(interface{}) error
}

type errorOption func(to interface{}) error

func (o errorOption) applyTo(to interface{}) error {
	return o(to)
}

var _ Option = errorOption(nil)

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

func (t *T) NewTestWorkspace() *v1beta1.Workspace {
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

func (t *T) NewAPIBinding(name string, options ...Option) *apisv1alpha1.APIBinding {
	return createAPIBinding(t, name, options...)
}

func (t *T) NewTestNamespace(options ...Option) *corev1.Namespace {
	namespace := createTestNamespace(t, options...)
	t.T().Cleanup(func() {
		deleteTestNamespace(t, namespace)
	})
	return namespace
}

func (t *T) NewSyncTarget(name string, options ...Option) *workloadv1alpha1.SyncTarget {
	workloadCluster, cleanup := createSyncTarget(t, name, options...)
	t.T().Cleanup(func() {
		deleteSyncTarget(t, workloadCluster)
	})
	t.T().Cleanup(func() {
		t.Expect(cleanup()).To(gomega.Succeed())
	})
	return workloadCluster
}
