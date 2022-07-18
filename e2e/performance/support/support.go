//go:build performance

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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TestTimeoutShort  = 1 * time.Minute
	TestTimeoutMedium = 5 * time.Minute
	TestTimeoutLong   = 10 * time.Minute

	TestDNSRecordCount = "TEST_DNSRECORD_COUNT"
	TestIngressCount   = "TEST_INGRESS_COUNT"

	DefaultTestDNSRecordCount = 1
	DefaultTestIngressCount   = 1
)

var (
	ApplyOptions = metav1.ApplyOptions{FieldManager: "kcp-glbc-e2e", Force: true}
)
