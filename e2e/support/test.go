//go:build e2e
// +build e2e

package support

import (
	"context"
	"sync"
	"testing"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

type Test interface {
	T() *testing.T
	Ctx() context.Context
	Client() Client

	Expect(actual interface{}, extra ...interface{}) types.Assertion
	Eventually(actual interface{}, intervals ...interface{}) types.AsyncAssertion
	Consistently(actual interface{}, intervals ...interface{}) types.AsyncAssertion

	WithNewTestWorkspace() *WithWorkspace
	WithNewTestNamespace(...NamespaceOption) *WithNamespace
}

func With(t *testing.T) Test {
	ctx := context.Background()
	if deadline, ok := t.Deadline(); ok {
		withDeadline, cancel := context.WithDeadline(ctx, deadline)
		t.Cleanup(cancel)
		ctx = withDeadline
	}

	return &T{
		WithT: gomega.NewWithT(t),
		t:     t,
		ctx:   ctx,
	}
}

type T struct {
	*gomega.WithT
	t      *testing.T
	ctx    context.Context
	client Client
	once   sync.Once
}

func (t *T) T() *testing.T {
	return t.t
}

func (t *T) Ctx() context.Context {
	return t.ctx
}

func (t *T) Client() Client {
	t.once.Do(func() {
		c, err := newTestClient()
		if err != nil {
			t.T().Fatalf("Error creating client: %v", err)
		}
		t.client = c
	})
	return t.client
}

func (t *T) Expect(actual interface{}, extra ...interface{}) types.Assertion {
	return t.WithT.Expect(actual, extra...)
}

func (t *T) Eventually(actual interface{}, intervals ...interface{}) types.AsyncAssertion {
	return t.WithT.Eventually(actual, intervals...)
}

func (t *T) Consistently(actual interface{}, intervals ...interface{}) types.AsyncAssertion {
	return t.WithT.Consistently(actual, intervals...)
}

func (t *T) WithNewTestWorkspace() *WithWorkspace {
	return &WithWorkspace{t}
}

func (t *T) WithNewTestNamespace(options ...NamespaceOption) *WithNamespace {
	return &WithNamespace{t, options}
}
