package v1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +crd
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
type DomainVerification struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DomainVerificationSpec `json:"spec"`
	// +optional
	Status DomainVerificationStatus `json:"status"`
}

func (dv *DomainVerification) GetToken() string {
	return fmt.Sprintf("glbctoken=glbc-%s", dv.ClusterName)
}

type DomainVerificationSpec struct {
	Domain string `json:"domain"`
}

type DomainVerificationStatus struct {
	Token    string `json:"token"`
	Verified bool   `json:"verified"`
	// +optional
	LastChecked metav1.Time `json:"lastChecked,omitempty"`
	// +optional
	NextCheck metav1.Time `json:"nextCheck,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type DomainVerificationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DomainVerification `json:"items"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DNSRecord is a DNS record managed by the HCG.
type DNSRecord struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the dnsRecord.
	Spec DNSRecordSpec `json:"spec"`
	// status is the most recently observed status of the dnsRecord.
	Status DNSRecordStatus `json:"status,omitempty"`
}

// GetProviderSpecificProperty returns a ProviderSpecificProperty if the property exists.
func (e *Endpoint) GetProviderSpecificProperty(key string) (ProviderSpecificProperty, bool) {
	for _, providerSpecific := range e.ProviderSpecific {
		if providerSpecific.Name == key {
			return providerSpecific, true
		}
	}
	return ProviderSpecificProperty{}, false
}

// SetID returns an id that should be unique across a set of endpoints
func (e *Endpoint) SetID() string {
	if e.SetIdentifier != "" {
		return e.SetIdentifier
	}
	return e.DNSName
}

// ProviderSpecificProperty holds the name and value of a configuration which is specific to individual DNS providers
type ProviderSpecificProperty struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// Targets is a representation of a list of targets for an endpoint.
type Targets []string

// TTL is a structure defining the TTL of a DNS record
type TTL int64

// Labels store metadata related to the endpoint
// it is then stored in a persistent storage via serialization
type Labels map[string]string

// ProviderSpecific holds configuration which is specific to individual DNS providers
type ProviderSpecific []ProviderSpecificProperty

// Endpoint is a high-level way of a connection between a service and an IP
type Endpoint struct {
	// The hostname of the DNS record
	DNSName string `json:"dnsName,omitempty"`
	// The targets the DNS record points to
	Targets Targets `json:"targets,omitempty"`
	// RecordType type of record, e.g. CNAME, A, SRV, TXT etc
	RecordType string `json:"recordType,omitempty"`
	// Identifier to distinguish multiple records with the same name and type (e.g. Route53 records with routing policies other than 'simple')
	SetIdentifier string `json:"setIdentifier,omitempty"`
	// TTL for the record
	RecordTTL TTL `json:"recordTTL,omitempty"`
	// Labels stores labels defined for the Endpoint
	// +optional
	Labels Labels `json:"labels,omitempty"`
	// ProviderSpecific stores provider specific config
	// +optional
	ProviderSpecific ProviderSpecific `json:"providerSpecific,omitempty"`
}

// DNSRecordSpec contains the details of a DNS record.
type DNSRecordSpec struct {
	Endpoints []*Endpoint `json:"endpoints,omitempty"`
}

// DNSRecordStatus is the most recently observed status of each record.
type DNSRecordStatus struct {
	// zones are the status of the record in each zone.
	Zones []DNSZoneStatus `json:"zones,omitempty"`

	// observedGeneration is the most recently observed generation of the
	// DNSRecord.  When the DNSRecord is updated, the controller updates the
	// corresponding record in each managed zone.  If an update for a
	// particular zone fails, that failure is recorded in the status
	// condition for the zone so that the controller can determine that it
	// needs to retry the update for that specific zone.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// DNSZone is used to define a DNS hosted zone.
// A zone can be identified by an ID or tags.
type DNSZone struct {
	// id is the identifier that can be used to find the DNS hosted zone.
	//
	// on AWS zone can be fetched using `ID` as id in [1]
	// on Azure zone can be fetched using `ID` as a pre-determined name in [2],
	// on GCP zone can be fetched using `ID` as a pre-determined name in [3].
	//
	// [1]: https://docs.aws.amazon.com/cli/latest/reference/route53/get-hosted-zone.html#options
	// [2]: https://docs.microsoft.com/en-us/cli/azure/network/dns/zone?view=azure-cli-latest#az-network-dns-zone-show
	// [3]: https://cloud.google.com/dns/docs/reference/v1/managedZones/get
	// +optional
	ID string `json:"id,omitempty"`

	// tags can be used to query the DNS hosted zone.
	//
	// on AWS, resourcegroupstaggingapi [1] can be used to fetch a zone using `Tags` as tag-filters,
	//
	// [1]: https://docs.aws.amazon.com/cli/latest/reference/resourcegroupstaggingapi/get-resources.html#options
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

// DNSZoneStatus is the status of a record within a specific zone.
type DNSZoneStatus struct {
	// dnsZone is the zone where the record is published.
	DNSZone DNSZone `json:"dnsZone"`
	// conditions are any conditions associated with the record in the zone.
	//
	// If publishing the record fails, the "Failed" condition will be set with a
	// reason and message describing the cause of the failure.
	Conditions []DNSZoneCondition `json:"conditions,omitempty"`
	// endpoints are the last endpoints that were successfully published to the provider
	//
	// Provides a simple mechanism to store the current provider records in order to
	// delete any that are no longer present in DNSRecordSpec.Endpoints
	//
	// Note: This will not be required if/when we switch to using external-dns since when
	// running with a "sync" policy it will clean up unused records automatically.
	Endpoints []*Endpoint `json:"endpoints,omitempty"`
}

var (
	// Failed means the record is not available within a zone.
	DNSRecordFailedConditionType = "Failed"
)

// DNSZoneCondition is just the standard condition fields.
type DNSZoneCondition struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +required
	Status             string      `json:"status"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	Reason             string      `json:"reason,omitempty"`
	Message            string      `json:"message,omitempty"`
}

// DNSRecordType is a DNS resource record type.
// +kubebuilder:validation:Enum=CNAME;A
type DNSRecordType string

const (
	// CNAMERecordType is an RFC 1035 CNAME record.
	CNAMERecordType DNSRecordType = "CNAME"

	// ARecordType is an RFC 1035 A record.
	ARecordType DNSRecordType = "A"
)

// +kubebuilder:object:root=true

// DNSRecordList contains a list of dnsrecords.
type DNSRecordList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSRecord `json:"items"`
}

func (endpoint *Endpoint) SetProviderSpecific(name, value string) {
	var property *ProviderSpecificProperty

	if endpoint.ProviderSpecific == nil {
		endpoint.ProviderSpecific = ProviderSpecific{}
	}

	for _, pair := range endpoint.ProviderSpecific {
		if pair.Name == name {
			property = &pair
		}
	}

	if property != nil {
		property.Value = value
		return
	}

	endpoint.ProviderSpecific = append(endpoint.ProviderSpecific, ProviderSpecificProperty{
		Name:  name,
		Value: value,
	})
}

func (endpoint *Endpoint) GetProviderSpecific(name string) (string, bool) {
	for _, property := range endpoint.ProviderSpecific {
		if property.Name == name {
			return property.Value, true
		}
	}

	return "", false
}

func (endpoint *Endpoint) DeleteProviderSpecific(name string) bool {
	if endpoint.ProviderSpecific == nil {
		return false
	}

	deleted := false
	providerSpecific := make(ProviderSpecific, 0, len(endpoint.ProviderSpecific))
	for _, pair := range endpoint.ProviderSpecific {
		if pair.Name == name {
			deleted = true
		} else {
			providerSpecific = append(providerSpecific, pair)
		}
	}

	endpoint.ProviderSpecific = providerSpecific
	return deleted
}

func (endpoint *Endpoint) GetAddress() (string, bool) {
	if endpoint.SetIdentifier == "" || len(endpoint.Targets) == 0 {
		return "", false
	}

	return string(endpoint.Targets[0]), true
}
