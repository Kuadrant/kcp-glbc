//go:build e2e
// +build e2e

package support

import (
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	tenancyhelper "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1/helper"
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
