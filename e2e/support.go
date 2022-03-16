package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kcp-dev/kcp/pkg/apis/cluster"
	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"
	conditionsapi "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"
	conditionsutil "github.com/kcp-dev/kcp/third_party/conditions/util/conditions"
)

const (
	TestTimeoutShort  = 1 * time.Minute
	TestTimeoutMedium = 5 * time.Minute
	TestTimeoutLong   = 10 * time.Minute

	AdminWorkspace = tenancyhelper.OrganizationCluster

	workloadClusterKubeConfigDir = "CLUSTERS_KUBECONFIG_DIR"
)

func init() {
	// Gomega settings
	gomega.SetDefaultEventuallyTimeout(TestTimeoutShort)
	// Disable object truncation on test results
	format.MaxLength = 0
}

type Test interface {
	T() *testing.T
	Ctx() context.Context
	Client() Client

	Expect(actual interface{}, extra ...interface{}) types.Assertion
	Eventually(actual interface{}, intervals ...interface{}) types.AsyncAssertion
	Consistently(actual interface{}, intervals ...interface{}) types.AsyncAssertion

	WithNewTestWorkspace() *WithWorkspace
	WithNewTestNamespace(...NamespaceOption) *WithNamespace
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

type WithWorkspace struct {
	Test
}

func (w *WithWorkspace) Do(f func(workspace *tenancyv1alpha1.Workspace)) {
	workspace := createTestWorkspace(w)
	defer deleteTestWorkspace(w, workspace)

	w.Eventually(Workspace(w, workspace.Name)).Should(
		gomega.WithTransform(
			ConditionStatus(tenancyv1alpha1.WorkspaceScheduled),
			gomega.Equal(corev1.ConditionTrue),
		))

	invokeWorkspaceTestCode(w, workspace, f)
}

type WithNamespace struct {
	Test
	options []NamespaceOption
}

func (n *WithNamespace) Do(f func(namespace *corev1.Namespace)) {
	namespace := createTestNamespace(n, n.options...)
	defer deleteTestNamespace(n, namespace)

	invokeNamespaceTestCode(n, namespace, f)
}

type NamespaceOption interface {
	ApplyTo(namespace *corev1.Namespace) error
}

type inWorkspace struct {
	workspace *tenancyv1alpha1.Workspace
}

func (o *inWorkspace) ApplyTo(namespace *corev1.Namespace) (err error) {
	namespace.ClusterName, err = tenancyhelper.EncodeLogicalClusterName(o.workspace)
	return
}

func InWorkspace(workspace *tenancyv1alpha1.Workspace) NamespaceOption {
	return &inWorkspace{workspace}
}

type withLabel struct {
	key, value string
}

func (o *withLabel) ApplyTo(namespace *corev1.Namespace) error {
	if namespace.Labels == nil {
		namespace.Labels = map[string]string{}
	}
	namespace.Labels[o.key] = o.value
	return nil
}

func WithLabel(key, value string) NamespaceOption {
	return &withLabel{key, value}
}

type withLabels struct {
	labels map[string]string
}

func (o *withLabels) ApplyTo(namespace *corev1.Namespace) error {
	namespace.Labels = o.labels
	return nil
}

func WithLabels(labels map[string]string) NamespaceOption {
	return &withLabels{labels}
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

func (t *T) Expect(actual interface{}, extra ...interface{}) types.Assertion {
	return t.WithT.Expect(actual, extra...)
}

func (t *T) Eventually(actual interface{}, intervals ...interface{}) types.AsyncAssertion {
	return t.WithT.Eventually(actual, intervals...)
}

func (t *T) Consistently(actual interface{}, intervals ...interface{}) types.AsyncAssertion {
	return t.WithT.Consistently(actual, intervals...)
}

func (t *T) WithNewTestWorkspace() *WithWorkspace {
	return &WithWorkspace{t}
}

func (t *T) WithNewTestNamespace(options ...NamespaceOption) *WithNamespace {
	return &WithNamespace{t, options}
}

func createTestWorkspace(t Test) *tenancyv1alpha1.Workspace {
	name := "test-" + uuid.New().String()

	workspace := &tenancyv1alpha1.Workspace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: tenancyv1alpha1.SchemeGroupVersion.String(),
			Kind:       "Workspace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: tenancyv1alpha1.WorkspaceSpec{
			InheritFrom: AdminWorkspace,
		},
	}

	workspace, err := t.Client().Kcp().Cluster(AdminWorkspace).TenancyV1alpha1().Workspaces().Create(t.Ctx(), workspace, metav1.CreateOptions{})
	if err != nil {
		t.Expect(err).NotTo(gomega.HaveOccurred())
	}

	return workspace
}

func deleteTestWorkspace(t Test, workspace *tenancyv1alpha1.Workspace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kcp().Cluster(workspace.ClusterName).TenancyV1alpha1().Workspaces().Delete(t.Ctx(), workspace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}

func createTestNamespace(t Test, options ...NamespaceOption) *corev1.Namespace {
	name := "test-" + uuid.New().String()

	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	for _, option := range options {
		t.Expect(option.ApplyTo(namespace)).To(gomega.Succeed())
	}

	namespace, err := t.Client().Core().Cluster(namespace.ClusterName).CoreV1().Namespaces().Create(t.Ctx(), namespace, metav1.CreateOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return namespace
}

func deleteTestNamespace(t Test, namespace *corev1.Namespace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Core().Cluster(namespace.ClusterName).CoreV1().Namespaces().Delete(t.Ctx(), namespace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}

func invokeNamespaceTestCode(t Test, namespace *corev1.Namespace, do func(namespace *corev1.Namespace)) {
	defer func() {
		// nolint: staticcheck
		if t.T().Failed() {
			// TODO
		}
	}()

	do(namespace)
}

func invokeWorkspaceTestCode(t Test, workspace *tenancyv1alpha1.Workspace, do func(*tenancyv1alpha1.Workspace)) {
	defer func() {
		// nolint: staticcheck
		if t.T().Failed() {
			// TODO
		}
	}()

	do(workspace)
}

func NewWorkloadClusterWithKubeConfig(name string) (*clusterv1alpha1.Cluster, error) {
	dir := os.Getenv(workloadClusterKubeConfigDir)
	if dir == "" {
		return nil, fmt.Errorf("%s environment variable is not set", workloadClusterKubeConfigDir)
	}

	data, err := ioutil.ReadFile(path.Join(dir, name+".yaml"))
	if err != nil {
		return nil, fmt.Errorf("error reading cluster %q Kubeconfig: %v", name, err)
	}

	workloadCluster := NewWorkloadCluster(name)
	workloadCluster.Spec.KubeConfig = string(data)

	return workloadCluster, nil
}

func NewWorkloadCluster(name string) *clusterv1alpha1.Cluster {
	return &clusterv1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cluster.GroupName,
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			// FIXME: workaround for https://github.com/kcp-dev/kcp/issues/730
			// Name: name,
			GenerateName: name + "-",
		},
	}
}

func Workspace(t Test, name string) func() *tenancyv1alpha1.Workspace {
	return func() *tenancyv1alpha1.Workspace {
		c, err := t.Client().Kcp().Cluster(AdminWorkspace).TenancyV1alpha1().Workspaces().Get(t.Ctx(), name, metav1.GetOptions{})
		t.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}

func WorkloadCluster(t Test, workspace, name string) func() *clusterv1alpha1.Cluster {
	return func() *clusterv1alpha1.Cluster {
		c, err := t.Client().Kcp().Cluster(workspace).ClusterV1alpha1().Clusters().Get(t.Ctx(), name, metav1.GetOptions{})
		t.Expect(err).NotTo(gomega.HaveOccurred())
		return c
	}
}

func ConditionStatus(conditionType conditionsapi.ConditionType) func(getter conditionsutil.Getter) corev1.ConditionStatus {
	return func(getter conditionsutil.Getter) corev1.ConditionStatus {
		c := conditionsutil.Get(getter, conditionType)
		if c == nil {
			return corev1.ConditionUnknown
		}
		return c.Status
	}
}
