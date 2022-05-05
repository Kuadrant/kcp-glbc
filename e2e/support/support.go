//go:build e2e
// +build e2e

package support

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
)

const (
	TestTimeoutShort  = 1 * time.Minute
	TestTimeoutMedium = 5 * time.Minute
	TestTimeoutLong   = 10 * time.Minute

	workloadClusterKubeConfigDir = "CLUSTERS_KUBECONFIG_DIR"
)

var (
	TestOrganization = tenancyv1alpha1.RootCluster.Join("default")
	ApplyOptions     = metav1.ApplyOptions{FieldManager: "kcp-glbc-e2e", Force: true}
)
