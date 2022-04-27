package net

import (
	"context"
	"time"

	"k8s.io/klog/v2"
)

// HostsWatcher keeps track of changes in host addresses in the background.
// It associates a host with a key that is passed to the `OnChange` callback
// whenever a change is detected
type HostsWatcher struct {
	Resolver      HostResolver
	Records       []recordWatcher
	OnChange      func(interface{})
	WatchInterval func(ttl time.Duration) time.Duration
}

func NewHostsWatcher(resolver HostResolver, watchInterval func(ttl time.Duration) time.Duration) *HostsWatcher {
	return &HostsWatcher{
		Resolver:      resolver,
		Records:       []recordWatcher{},
		WatchInterval: watchInterval,
	}
}

func (w *HostsWatcher) ListHostRecordWatchers(obj interface{}) []recordWatcher {
	var recordWatchers []recordWatcher
	for _, record := range w.Records {
		if obj == record.Key {
			recordWatchers = append(recordWatchers, record)
		}
	}
	return recordWatchers
}

// StartWatching begins tracking changes in the addresses for host
func (w *HostsWatcher) StartWatching(ctx context.Context, obj interface{}, host string) bool {
	for _, recordWatcher := range w.Records {
		if recordWatcher.Key == obj && recordWatcher.Host == host {
			return false
		}
	}

	c, cancel := context.WithCancel(ctx)

	recordWatcher := recordWatcher{
		cancel:        cancel,
		resolver:      w.Resolver,
		Host:          host,
		Key:           obj,
		OnChange:      w.OnChange,
		Records:       []HostAddress{},
		WatchInterval: w.WatchInterval,
	}
	recordWatcher.watch(c)

	w.Records = append(w.Records, recordWatcher)

	klog.V(3).Infof("Started watching %s with host %s", obj, host)
	return true
}

// StopWatching stops tracking changes in the addresses associated to obj
func (w *HostsWatcher) StopWatching(obj interface{}, host string) {
	var records []recordWatcher
	for _, recordWatcher := range w.Records {
		if (host == "" || host == recordWatcher.Host) && recordWatcher.Key == obj {
			recordWatcher.stop()
			continue
		}
		records = append(records, recordWatcher)
	}
	w.Records = records
}

type recordWatcher struct {
	resolver      HostResolver
	cancel        context.CancelFunc
	Key           interface{}
	OnChange      func(key interface{})
	WatchInterval func(ttl time.Duration) time.Duration
	Host          string
	Records       []HostAddress
}

func DefaultInterval(ttl time.Duration) time.Duration {
	return ttl / 2
}

func (w *recordWatcher) watch(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			newRecords, err := w.resolver.LookupIPAddr(ctx, w.Host)
			if err != nil {
				klog.Error(err)
				continue
			}

			if updated := w.updateRecords(newRecords); updated {
				klog.V(3).Infof("New records found for host %s. Updating", w.Host)
				w.OnChange(w.Key)
			}

			ttl := w.Records[0].TTL
			refreshInterval := w.WatchInterval(ttl)
			time.Sleep(refreshInterval)
			klog.V(4).Infof("Refreshing records for host %s with TTL %d. Refresh interval: %d", w.Host, int(ttl.Seconds()), int(refreshInterval.Seconds()))
		}
	}()
}

func (w *recordWatcher) updateRecords(newRecords []HostAddress) bool {
	if len(w.Records) != len(newRecords) {
		w.Records = newRecords
		return true
	}

	updatedIPs := false
	updatedTTLs := false

	for i, newRecord := range newRecords {
		if !w.Records[i].IP.Equal(newRecord.IP) {
			updatedIPs = true
			continue
		}

		if w.Records[i].TTL < newRecord.TTL {
			updatedTTLs = true
		}
	}

	if updatedIPs || updatedTTLs {
		w.Records = newRecords
	}

	return updatedIPs
}

func (w *recordWatcher) stop() {
	klog.V(3).Infof("Stopping record watcher for %s/%s", w.Key, w.Host)
	w.cancel()
}
