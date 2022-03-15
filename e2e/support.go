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
	conditionsapi "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"
	conditionsutil "github.com/kcp-dev/kcp/third_party/conditions/util/conditions"
)

const (
	TestTimeoutShort  = 1 * time.Minute
	TestTimeoutMedium = 5 * time.Minute
	TestTimeoutLong   = 10 * time.Minute

	AdminWorkspace = "admin"

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

func WithT(t *testing.T) Test {
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

func WithNewTestWorkspace(t Test, doRun func(workspace *tenancyv1alpha1.Workspace)) {
	workspace := CreateTestWorkspace(t)
	defer DeleteTestWorkspace(t, workspace)

	t.Eventually(Workspace(t, workspace.Name)).Should(
		gomega.WithTransform(
			ConditionStatus(tenancyv1alpha1.WorkspaceScheduled),
			gomega.Equal(corev1.ConditionTrue),
		))

	invokeWorkspaceTestCode(t, workspace, doRun)
}

func CreateTestWorkspace(t Test) *tenancyv1alpha1.Workspace {
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

func DeleteTestWorkspace(t Test, workspace *tenancyv1alpha1.Workspace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Kcp().Cluster(workspace.ClusterName).TenancyV1alpha1().Workspaces().Delete(t.Ctx(), workspace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}

// func WithNewTestNamespace(t Test, doRun func(string)) {
// 	namespace := CreateTestNamespace(t)
// 	defer DeleteTestNamespace(t, namespace)
//
// 	invokeNamespaceTestCode(t, namespace.GetName(), doRun)
// }

func CreateTestNamespace(t Test, workspace string) *corev1.Namespace {
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

	namespace, err := t.Client().Core().Cluster(workspace).CoreV1().Namespaces().Create(t.Ctx(), namespace, metav1.CreateOptions{})
	t.Expect(err).NotTo(gomega.HaveOccurred())

	return namespace
}

func DeleteTestNamespace(t Test, namespace *corev1.Namespace) {
	propagationPolicy := metav1.DeletePropagationBackground
	err := t.Client().Core().Cluster(namespace.ClusterName).CoreV1().Namespaces().Delete(t.Ctx(), namespace.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	t.Expect(err).NotTo(gomega.HaveOccurred())
}

func invokeNamespaceTestCode(t Test, namespace string, doRun func(string)) {
	defer func() {
		if t.T().Failed() {
			// TODO
		}
	}()

	doRun(namespace)
}

func invokeWorkspaceTestCode(t Test, workspace *tenancyv1alpha1.Workspace, doRun func(*tenancyv1alpha1.Workspace)) {
	defer func() {
		if t.T().Failed() {
			// TODO
		}
	}()

	doRun(workspace)
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
			Name: name,
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
