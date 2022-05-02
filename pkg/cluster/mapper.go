package cluster

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kuadrant/kcp-glbc/pkg/tls"
)

const (
	// TODO: consider grouping the annotations below into a single structured annotation
	LABEL_HCG_HOST                      = "kuadrant.dev/hcg.host"
	ANNOTATION_HCG_WORKSPACE            = "kuadrant.dev/hcg.workspace"
	ANNOTATION_HCG_NAMESPACE            = "kuadrant.dev/hcg.namespace"
	LABEL_HCG_MANAGED                   = "kuadrant.dev/hcg.managed"
	ANNOTATION_HCG_HOST                 = "kuadrant.dev/host.generated"
	ANNOTATION_HCG_CUSTOM_HOST_REPLACED = "kuadrant.dev/custom-hosts.replaced"
	creationTimestampAnnotation         = "kuadrant.dev/hcg.creationTimestamp"
	LABEL_OWNED_BY                      = "kcp.dev/owned-by"

	ANNOTATION_HEALTH_CHECK_PREFIX = "kuadrant.experimental/health-"
)

type context struct {
	workspace         string
	namespace         string
	name              string
	host              string
	ownedBy           string
	creationTimestamp metav1.Time
}

var _ ObjectMapper = &context{}
var _ tls.CertificateRequest = &context{}

type ObjectMapper interface {
	Name() string
	Namespace() string
	Workspace() string
	CreationTimestamp() metav1.Time
	Host() string
	Labels() map[string]string
	Annotations() map[string]string
	OwnedBy() string
}

var noContextErr = errors.New("object is missing needed context")

func IsNoContextErr(err error) bool {
	return errors.Is(err, noContextErr)
}

// NewKCPObjectMapper will return an object that can map a resource in the control cluster to an
// object in KCP based on the annotations applied to it in the control cluster. It will fail if the annotations are missing
func NewKCPObjectMapper(obj runtime.Object) (ObjectMapper, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	kcpContext := &context{
		name: accessor.GetName(),
	}
	annotations := accessor.GetAnnotations()
	labels := accessor.GetLabels()

	v, ok := annotations[creationTimestampAnnotation]
	if !ok || v == "" {
		return nil, fmt.Errorf("%w expected annotation %s to be present and not empty", noContextErr, creationTimestampAnnotation)
	}
	pt, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, err
	}
	kcpContext.creationTimestamp = metav1.Time{Time: pt}

	v, ok = annotations[ANNOTATION_HCG_WORKSPACE]
	if !ok || v == "" {
		return nil, fmt.Errorf("%w expected annotation %s to be present and not empty ", noContextErr, ANNOTATION_HCG_WORKSPACE)
	}
	kcpContext.workspace = v

	v, ok = annotations[ANNOTATION_HCG_NAMESPACE]
	if !ok || v == "" {
		return nil, fmt.Errorf("%w expected annotation %s to be present and not empty", noContextErr, ANNOTATION_HCG_NAMESPACE)
	}
	kcpContext.namespace = v

	v, ok = annotations[ANNOTATION_HCG_HOST]
	if !ok || v == "" {
		return nil, fmt.Errorf("%w expected annotation %s to be present and not empty", noContextErr, ANNOTATION_HCG_HOST)
	}
	kcpContext.host = v

	kcpContext.ownedBy = labels[LABEL_OWNED_BY]

	return kcpContext, nil
}

func (kc context) CreationTimestamp() metav1.Time {
	return kc.creationTimestamp
}

func (kc context) OwnedBy() string {
	return kc.ownedBy
}

func (kc *context) Name() string {
	return kc.name
}

func (kc *context) Annotations() map[string]string {
	return map[string]string{
		ANNOTATION_HCG_WORKSPACE:    kc.workspace,
		ANNOTATION_HCG_NAMESPACE:    kc.namespace,
		ANNOTATION_HCG_HOST:         kc.host,
		creationTimestampAnnotation: kc.creationTimestamp.Format(time.RFC3339),
	}
}

func (kc *context) Labels() map[string]string {
	return map[string]string{
		LABEL_HCG_HOST:    kc.host,
		LABEL_HCG_MANAGED: "true",
		LABEL_OWNED_BY:    kc.ownedBy,
	}
}

func (kc *context) Host() string {
	return kc.host
}

func (kc *context) Namespace() string {
	return kc.namespace
}

func (kc *context) Workspace() string {
	return kc.workspace
}

type controlContext struct {
	*context
}

// NewControlObjectMapper returns an object that can map from something in the KCP API
// to something in the control cluster. It provides a set of Labels and Annotations to apply to an object
// that will be created in the control cluster to enable them to be mapped back.
// It expects a Host annotation.
func NewControlObjectMapper(obj runtime.Object) (ObjectMapper, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	host, ok := accessor.GetAnnotations()[ANNOTATION_HCG_HOST]
	if !ok || host == "" {
		return nil, fmt.Errorf("%w expected annotation %s to be present and not empty", noContextErr, ANNOTATION_HCG_HOST)
	}
	return &controlContext{
		context: &context{
			workspace:         accessor.GetClusterName(),
			namespace:         accessor.GetNamespace(),
			name:              accessor.GetName(),
			creationTimestamp: accessor.GetCreationTimestamp(),
			ownedBy:           accessor.GetName(), // this is the object context
			host:              host,
		},
	}, nil
}

func (cr *controlContext) Name() string {
	// Removes chars which are invalid characters for cert manager certificate names. RFC 1123 subdomain must consist of
	// lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character
	return strings.ReplaceAll(fmt.Sprintf("%s-%s-%s", cr.Workspace(), cr.Namespace(), cr.name), ":", "")
}

func (cr *controlContext) Host() string {
	return cr.host
}

func (cr *controlContext) Labels() map[string]string {
	return map[string]string{
		LABEL_HCG_HOST:    cr.host,
		LABEL_HCG_MANAGED: "true",
		LABEL_OWNED_BY:    cr.ownedBy,
	}
}
