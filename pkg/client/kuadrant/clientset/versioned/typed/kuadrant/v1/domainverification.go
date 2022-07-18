// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	logicalcluster "github.com/kcp-dev/logicalcluster"
	v1 "github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1"
	scheme "github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// DomainVerificationsGetter has a method to return a DomainVerificationInterface.
// A group's client should implement this interface.
type DomainVerificationsGetter interface {
	DomainVerifications() DomainVerificationInterface
}

// DomainVerificationInterface has methods to work with DomainVerification resources.
type DomainVerificationInterface interface {
	Create(ctx context.Context, domainVerification *v1.DomainVerification, opts metav1.CreateOptions) (*v1.DomainVerification, error)
	Update(ctx context.Context, domainVerification *v1.DomainVerification, opts metav1.UpdateOptions) (*v1.DomainVerification, error)
	UpdateStatus(ctx context.Context, domainVerification *v1.DomainVerification, opts metav1.UpdateOptions) (*v1.DomainVerification, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.DomainVerification, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.DomainVerificationList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.DomainVerification, err error)
	DomainVerificationExpansion
}

// domainVerifications implements DomainVerificationInterface
type domainVerifications struct {
	client  rest.Interface
	cluster logicalcluster.Name
}

// newDomainVerifications returns a DomainVerifications
func newDomainVerifications(c *KuadrantV1Client) *domainVerifications {
	return &domainVerifications{
		client:  c.RESTClient(),
		cluster: c.cluster,
	}
}

// Get takes name of the domainVerification, and returns the corresponding domainVerification object, and an error if there is any.
func (c *domainVerifications) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.DomainVerification, err error) {
	result = &v1.DomainVerification{}
	err = c.client.Get().
		Cluster(c.cluster).
		Resource("domainverifications").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of DomainVerifications that match those selectors.
func (c *domainVerifications) List(ctx context.Context, opts metav1.ListOptions) (result *v1.DomainVerificationList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.DomainVerificationList{}
	err = c.client.Get().
		Cluster(c.cluster).
		Resource("domainverifications").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested domainVerifications.
func (c *domainVerifications) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Cluster(c.cluster).
		Resource("domainverifications").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a domainVerification and creates it.  Returns the server's representation of the domainVerification, and an error, if there is any.
func (c *domainVerifications) Create(ctx context.Context, domainVerification *v1.DomainVerification, opts metav1.CreateOptions) (result *v1.DomainVerification, err error) {
	result = &v1.DomainVerification{}
	err = c.client.Post().
		Cluster(c.cluster).
		Resource("domainverifications").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(domainVerification).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a domainVerification and updates it. Returns the server's representation of the domainVerification, and an error, if there is any.
func (c *domainVerifications) Update(ctx context.Context, domainVerification *v1.DomainVerification, opts metav1.UpdateOptions) (result *v1.DomainVerification, err error) {
	result = &v1.DomainVerification{}
	err = c.client.Put().
		Cluster(c.cluster).
		Resource("domainverifications").
		Name(domainVerification.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(domainVerification).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *domainVerifications) UpdateStatus(ctx context.Context, domainVerification *v1.DomainVerification, opts metav1.UpdateOptions) (result *v1.DomainVerification, err error) {
	result = &v1.DomainVerification{}
	err = c.client.Put().
		Cluster(c.cluster).
		Resource("domainverifications").
		Name(domainVerification.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(domainVerification).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the domainVerification and deletes it. Returns an error if one occurs.
func (c *domainVerifications) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Cluster(c.cluster).
		Resource("domainverifications").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *domainVerifications) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Cluster(c.cluster).
		Resource("domainverifications").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched domainVerification.
func (c *domainVerifications) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.DomainVerification, err error) {
	result = &v1.DomainVerification{}
	err = c.client.Patch(pt).
		Cluster(c.cluster).
		Resource("domainverifications").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
