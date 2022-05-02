package dns

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"

	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	"github.com/kuadrant/kcp-glbc/pkg/cluster"
	"github.com/kuadrant/kcp-glbc/pkg/dns"
)

// healthChecksConfig represents the user configuration for the health checks
type healthChecksConfig struct {
	Endpoint         string
	Port             *int64
	FailureThreshold *int64
	Protocol         *dns.HealthCheckProtocol
}

// annotationsConfigMap contains the logic to map an annotation-based configuration
// value into a mutation of the healthChecksConfig.
var annotationsConfigMap = map[string]func(string, *healthChecksConfig) error{
	"endpoint": notNilConfig(func(endpoint string, s *healthChecksConfig) error {
		s.Endpoint = endpoint
		return nil
	}),
	"port": notNilConfig(configInt64(func(port int64, c *healthChecksConfig) {
		c.Port = &port
	})),
	"protocol": notNilConfig(func(protocol string, c *healthChecksConfig) error {
		var value dns.HealthCheckProtocol
		switch protocol {
		case string(dns.HealthCheckProtocolHTTP):
		case string(dns.HealthCheckProtocolHTTPS):
			value = dns.HealthCheckProtocol(protocol)
		}

		if value == "" {
			return fmt.Errorf("invalid protocol %s. Only supported values are HTTP and HTTPS", protocol)
		}

		c.Protocol = &value
		return nil
	}),
	"failure-threshold": notNilConfig(configInt64(func(v int64, c *healthChecksConfig) {
		c.FailureThreshold = &v
	})),
}

func (c *Controller) ReconcileHealthChecks(ctx context.Context, dnsRecord *v1.DNSRecord) error {
	config, err := configFromAnnotations(dnsRecord.Annotations)
	if err != nil {
		return err
	}

	if config == nil {
		return c.reconcileHealthCheckDeletion(ctx, dnsRecord)
	}

	if err = validateHealthChecksConfig(config); err != nil {
		return err
	}

	return c.reconcileHealthCheck(ctx, config, dnsRecord)
}

func (c *Controller) reconcileHealthCheck(ctx context.Context, config *healthChecksConfig, dnsRecord *v1.DNSRecord) error {
	healthCheck := c.dnsProvider.HealthCheckReconciler()

	for _, dnsEndpoint := range dnsRecord.Spec.Endpoints {
		ok := false
		if _, ok = dnsEndpoint.GetAddress(); !ok {
			c.Logger.Info("Skipping health check creation: no address set", "record", dnsRecord, "endpoint", dnsEndpoint.DNSName)
			continue
		}

		endpointId, err := idForEndpoint(dnsRecord, dnsEndpoint)
		if err != nil {
			return err
		}

		spec := dns.HealthCheckSpec{
			Id:               endpointId,
			Name:             fmt.Sprintf("%s-%s", dnsEndpoint.DNSName, dnsEndpoint.SetIdentifier),
			Path:             config.Endpoint,
			Port:             config.Port,
			Protocol:         config.Protocol,
			FailureThreshold: config.FailureThreshold,
		}

		c.Logger.Info("Reconciling health check for endpoint", "name", dnsEndpoint.DNSName, "identifier", dnsEndpoint.SetIdentifier)

		err = healthCheck.Reconcile(ctx, spec, dnsEndpoint)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) reconcileHealthCheckDeletion(ctx context.Context, dnsRecord *v1.DNSRecord) error {
	reconciler := c.dnsProvider.HealthCheckReconciler()

	for _, zone := range dnsRecord.Status.Zones {
		for _, endpoint := range zone.Endpoints {
			if err := reconciler.Delete(ctx, endpoint); err != nil {
				return err
			}
		}
	}

	return nil
}

// idForEndpoint returns a unique identifier for an endpoint
func idForEndpoint(dnsRecord *v1.DNSRecord, endpoint *v1.Endpoint) (string, error) {
	hash := md5.New()
	if _, err := io.WriteString(hash, fmt.Sprintf("%s/%s@%s", dnsRecord.Name, endpoint.SetIdentifier, endpoint.DNSName)); err != nil {
		return "", fmt.Errorf("unexpected error creating ID for endpoint %s", endpoint.SetIdentifier)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// configFromAnnotations populates a healthChecksConfig instance from the
// annotations map. If no relevant annotation are found, returns nil
func configFromAnnotations(annotations map[string]string) (*healthChecksConfig, error) {
	if annotations == nil {
		return nil, nil
	}

	var result *healthChecksConfig

	for k, v := range annotations {
		// The annotation is not for the health check. Skip it
		if !strings.HasPrefix(k, cluster.ANNOTATION_HEALTH_CHECK_PREFIX) {
			continue
		}

		// Get the configuration key
		field := strings.TrimPrefix(k, cluster.ANNOTATION_HEALTH_CHECK_PREFIX)

		// Get the set value function for the configuration key
		setValue, ok := annotationsConfigMap[field]
		if !ok {
			return nil, fmt.Errorf("invalid value for annotation %s: %s", k, v)
		}

		if result == nil {
			result = &healthChecksConfig{}
		}

		// Apply the value into the resulting configuration instance
		if err := setValue(v, result); err != nil {
			return nil, fmt.Errorf("invalid value for annotation %s: %v", k, err)
		}

	}

	return result, nil
}

func validateHealthChecksConfig(config *healthChecksConfig) error {
	if config == nil {
		return errors.New("health checks config can't be nil")
	}

	if config.Endpoint == "" {
		return errors.New("endpoint is a required value to configure health checks")
	}
	if config.Port == nil {
		config.Port = aws.Int64(80)
	}
	if config.Protocol == nil {
		defaultProtocol := dns.HealthCheckProtocolHTTP
		config.Protocol = &defaultProtocol
	}

	return nil
}

func configInt64(f func(int64, *healthChecksConfig)) func(string, *healthChecksConfig) error {
	return func(s string, c *healthChecksConfig) error {
		intValue, err := strconv.Atoi(s)
		if err != nil {
			return err
		}

		f(int64(intValue), c)
		return nil
	}
}

func notNilConfig(f func(string, *healthChecksConfig) error) func(string, *healthChecksConfig) error {
	return func(s string, hcc *healthChecksConfig) error {
		if hcc == nil {
			return errors.New("health check config can't be nil")
		}

		return f(s, hcc)
	}
}
