package dns

import (
	"context"
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilclock "k8s.io/apimachinery/pkg/util/clock"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/kcp-dev/apimachinery/pkg/logicalcluster"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/util/slice"
)

type ConditionStatus string

const (
	DNSRecordFinalizer = "kuadrant.dev/dns-record"

	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

func (c *Controller) reconcile(ctx context.Context, dnsRecord *v1.DNSRecord) error {
	c.Logger.Info("Reconciling DNSRecord", "dnsRecord", dnsRecord)

	// If the DNS record was deleted, clean up and return.
	if dnsRecord.DeletionTimestamp != nil && !dnsRecord.DeletionTimestamp.IsZero() {
		if err := c.reconcileHealthCheckDeletion(ctx, dnsRecord); err != nil {
			return err
		}

		c.Logger.Info("Deleting DNSRecord", "dnsRecord", dnsRecord)
		if err := c.deleteRecord(dnsRecord); err != nil {
			c.Logger.Error(err, "Failed to delete DNSRecord", "record", dnsRecord)
			return err
		}

		return nil
	}

	if !slice.ContainsString(dnsRecord.Finalizers, DNSRecordFinalizer) {
		dnsRecord.Finalizers = append(dnsRecord.Finalizers, DNSRecordFinalizer)
	}

	statuses := c.publishRecordToZones(c.dnsZones, dnsRecord)
	if !dnsZoneStatusSlicesEqual(statuses, dnsRecord.Status.Zones) || dnsRecord.Status.ObservedGeneration != dnsRecord.Generation {
		dnsRecord.Status.Zones = statuses
		dnsRecord.Status.ObservedGeneration = dnsRecord.Generation
		_, err := c.dnsRecordClient.Cluster(logicalcluster.From(dnsRecord)).KuadrantV1().DNSRecords(dnsRecord.Namespace).UpdateStatus(ctx, dnsRecord, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	if err := c.ReconcileHealthChecks(ctx, dnsRecord); err != nil {
		c.Logger.Error(err, "Failed to reconcile health check for DNSRecord", "record", dnsRecord)
		return err
	}

	return nil
}

func (c *Controller) publishRecordToZones(zones []v1.DNSZone, record *v1.DNSRecord) []v1.DNSZoneStatus {
	var statuses []v1.DNSZoneStatus
	for i := range zones {
		zone := zones[i]

		// Only publish the record if the DNSRecord has been modified
		// (which would mean the target could have changed) or its
		// status does not indicate that it has already been published.
		if record.Generation == record.Status.ObservedGeneration && recordIsAlreadyPublishedToZone(record, &zone) {
			c.Logger.Info("Skipping zone to which the DNS record is already published", "record", record, "zone", zone)
			continue
		}

		condition := v1.DNSZoneCondition{
			Status:             string(ConditionUnknown),
			Type:               v1.DNSRecordFailedConditionType,
			LastTransitionTime: metav1.Now(),
		}

		if recordIsAlreadyPublishedToZone(record, &zone) {
			c.Logger.Info("replacing DNS record", "record", record, "zone", zone)

			if err := c.dnsProvider.Ensure(record, zone); err != nil {
				c.Logger.Error(err, "Failed to replace DNS record in zone", "record", record.Spec, "zone", zone)
				condition.Status = string(ConditionTrue)
				condition.Reason = "ProviderError"
				condition.Message = fmt.Sprintf("The DNS provider failed to replace the record: %v", err)
			} else {
				c.Logger.Info("Replaced DNS record in zone", "record", record.Spec, "zone", zone)
				condition.Status = string(ConditionFalse)
				condition.Reason = "ProviderSuccess"
				condition.Message = "The DNS provider succeeded in replacing the record"
			}
		} else {
			if err := c.dnsProvider.Ensure(record, zone); err != nil {
				c.Logger.Error(err, "Failed to publish DNS record to zone", "record", record.Spec, "zone", zone)
				condition.Status = string(ConditionTrue)
				condition.Reason = "ProviderError"
				condition.Message = fmt.Sprintf("The DNS provider failed to ensure the record: %v", err)
			} else {
				c.Logger.Info("Published DNS record to zone", "record", record.Spec, "zone", zone)
				condition.Status = string(ConditionFalse)
				condition.Reason = "ProviderSuccess"
				condition.Message = "The DNS provider succeeded in ensuring the record"
			}
		}
		statuses = append(statuses, v1.DNSZoneStatus{
			DNSZone:    zone,
			Conditions: []v1.DNSZoneCondition{condition},
			Endpoints:  record.Spec.Endpoints,
		})
	}
	return mergeStatuses(zones, record.Status.DeepCopy().Zones, statuses)
}

func (c *Controller) deleteRecord(record *v1.DNSRecord) error {
	var errs []error
	for i := range record.Status.Zones {
		zone := record.Status.Zones[i].DNSZone
		// If the record is currently not published in a zone,
		// skip deleting it for that zone.
		if !recordIsAlreadyPublishedToZone(record, &zone) {
			continue
		}
		err := c.dnsProvider.Delete(record, zone)
		if err != nil {
			errs = append(errs, err)
		} else {
			c.Logger.Info("Deleted DNSRecord from DNS provider", "record", record.Spec, "zone", zone)
		}
	}
	if len(errs) == 0 {
		if slice.ContainsString(record.Finalizers, DNSRecordFinalizer) {
			record.Finalizers = slice.RemoveString(record.Finalizers, DNSRecordFinalizer)
		}
	}
	return utilerrors.NewAggregate(errs)
}

// recordIsAlreadyPublishedToZone returns a Boolean value indicating whether the
// given DNSRecord is already published to the given zone, as determined from
// the DNSRecord's status conditions.
func recordIsAlreadyPublishedToZone(record *v1.DNSRecord, zoneToPublish *v1.DNSZone) bool {
	for _, zoneInStatus := range record.Status.Zones {
		if !reflect.DeepEqual(&zoneInStatus.DNSZone, zoneToPublish) {
			continue
		}

		for _, condition := range zoneInStatus.Conditions {
			if condition.Type == v1.DNSRecordFailedConditionType {
				return condition.Status == string(ConditionFalse)
			}
		}
	}

	return false
}

// mergeStatuses updates or extends the provided slice of statuses with the
// provided updates and returns the resulting slice.
func mergeStatuses(zones []v1.DNSZone, statuses, updates []v1.DNSZoneStatus) []v1.DNSZoneStatus {
	var additions []v1.DNSZoneStatus
	for i, update := range updates {
		add := true
		for j, status := range statuses {
			if cmp.Equal(status.DNSZone, update.DNSZone) {
				add = false
				statuses[j].Conditions = mergeConditions(status.Conditions, update.Conditions)
				statuses[j].Endpoints = update.Endpoints
			}
		}
		if add {
			additions = append(additions, updates[i])
		}
	}
	return append(statuses, additions...)
}

// clock is to enable unit testing
var clock utilclock.Clock = utilclock.RealClock{}

// mergeConditions adds or updates matching conditions, and updates
// the transition time if details of a condition have changed. Returns
// the updated condition array.
func mergeConditions(conditions, updates []v1.DNSZoneCondition) []v1.DNSZoneCondition {
	now := metav1.NewTime(clock.Now())
	var additions []v1.DNSZoneCondition
	for i, update := range updates {
		add := true
		for j, cond := range conditions {
			if cond.Type == update.Type {
				add = false
				if conditionChanged(cond, update) {
					conditions[j].Status = update.Status
					conditions[j].Reason = update.Reason
					conditions[j].Message = update.Message
					conditions[j].LastTransitionTime = now
					break
				}
			}
		}
		if add {
			updates[i].LastTransitionTime = now
			additions = append(additions, updates[i])
		}
	}
	conditions = append(conditions, additions...)
	return conditions
}

func conditionChanged(a, b v1.DNSZoneCondition) bool {
	return a.Status != b.Status || a.Reason != b.Reason
}

// dnsZoneStatusSlicesEqual compares two DNSZoneStatus slice values.  Returns
// true if the provided values should be considered equal for the purpose of
// determining whether an update is necessary, false otherwise.  The comparison
// is agnostic with respect to the ordering of status conditions but not with
// respect to zones.
func dnsZoneStatusSlicesEqual(a, b []v1.DNSZoneStatus) bool {
	conditionCmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.SortSlices(func(a, b v1.DNSZoneCondition) bool {
			return a.Type < b.Type
		}),
	}
	return cmp.Equal(a, b, conditionCmpOpts...)
}
