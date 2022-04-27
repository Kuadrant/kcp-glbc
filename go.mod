module github.com/kuadrant/kcp-glbc

go 1.16

require (
	github.com/aws/aws-sdk-go v1.40.21
	github.com/google/go-cmp v0.5.6
	github.com/google/uuid v1.3.0
	github.com/jetstack/cert-manager v1.7.1
	github.com/kcp-dev/apimachinery v0.0.0-20220326161941-461c06dd2b07
	github.com/kcp-dev/kcp v0.3.0-beta.1
	github.com/miekg/dns v1.1.34
	github.com/onsi/gomega v1.17.0
	github.com/prometheus/client_golang v1.11.0
	github.com/rs/xid v1.3.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	k8s.io/api v0.23.1
	k8s.io/apimachinery v0.23.5
	k8s.io/apiserver v0.23.1
	k8s.io/client-go v0.23.1
	k8s.io/code-generator v0.23.1
	k8s.io/klog/v2 v2.30.0
	k8s.io/utils v0.0.0-20211208161948-7d6a63dca704
)

replace (
	k8s.io/api => github.com/kcp-dev/kubernetes/staging/src/k8s.io/api v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/apiextensions-apiserver => github.com/kcp-dev/kubernetes/staging/src/k8s.io/apiextensions-apiserver v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/apimachinery => github.com/kcp-dev/kubernetes/staging/src/k8s.io/apimachinery v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/apiserver => github.com/kcp-dev/kubernetes/staging/src/k8s.io/apiserver v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/cli-runtime => github.com/kcp-dev/kubernetes/staging/src/k8s.io/cli-runtime v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/client-go => github.com/kcp-dev/kubernetes/staging/src/k8s.io/client-go v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/cloud-provider => github.com/kcp-dev/kubernetes/staging/src/k8s.io/cloud-provider v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/cluster-bootstrap => github.com/kcp-dev/kubernetes/staging/src/k8s.io/cluster-bootstrap v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/code-generator => github.com/kcp-dev/kubernetes/staging/src/k8s.io/code-generator v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/component-base => github.com/kcp-dev/kubernetes/staging/src/k8s.io/component-base v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/component-helpers => github.com/kcp-dev/kubernetes/staging/src/k8s.io/component-helpers v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/controller-manager => github.com/kcp-dev/kubernetes/staging/src/k8s.io/controller-manager v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/cri-api => github.com/kcp-dev/kubernetes/staging/src/k8s.io/cri-api v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/csi-translation-lib => github.com/kcp-dev/kubernetes/staging/src/k8s.io/csi-translation-lib v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kube-aggregator => github.com/kcp-dev/kubernetes/staging/src/k8s.io/kube-aggregator v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kube-controller-manager => github.com/kcp-dev/kubernetes/staging/src/k8s.io/kube-controller-manager v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kube-proxy => github.com/kcp-dev/kubernetes/staging/src/k8s.io/kube-proxy v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kube-scheduler => github.com/kcp-dev/kubernetes/staging/src/k8s.io/kube-scheduler v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kubectl => github.com/kcp-dev/kubernetes/staging/src/k8s.io/kubectl v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kubelet => github.com/kcp-dev/kubernetes/staging/src/k8s.io/kubelet v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/kubernetes => github.com/kcp-dev/kubernetes v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/legacy-cloud-providers => github.com/kcp-dev/kubernetes/staging/src/k8s.io/legacy-cloud-providers v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/metrics => github.com/kcp-dev/kubernetes/staging/src/k8s.io/metrics v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/mount-utils => github.com/kcp-dev/kubernetes/staging/src/k8s.io/mount-utils v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/pod-security-admission => github.com/kcp-dev/kubernetes/staging/src/k8s.io/pod-security-admission v0.0.0-20220326094338-57ce33e7efbb
	k8s.io/sample-apiserver => github.com/kcp-dev/kubernetes/staging/src/k8s.io/sample-apiserver v0.0.0-20220326094338-57ce33e7efbb
)
