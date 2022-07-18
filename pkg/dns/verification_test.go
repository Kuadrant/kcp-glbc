package dns_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kuadrant/kcp-glbc/pkg/dns"
)

type mockResolver struct {
	err   error
	value []string
}

func (mr *mockResolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	return mr.value, mr.err
}

func TestTxtRecordExists(t *testing.T) {
	expectedRecordValue := "glbctoken=glbc-root:default:kcp-glbc"
	cases := []struct {
		Name        string
		ExpectErr   bool
		Resolver    *mockResolver
		ShouldExist bool
	}{
		{
			Name: "should return false on error",
			Resolver: &mockResolver{
				value: []string{},
				err:   errors.New("failed to look up could not resolve host "),
			},
			ExpectErr: true,
		},
		{
			Name: "should return false when value not present",
			Resolver: &mockResolver{
				value: []string{"someothervalue"},
				err:   nil,
			},
		},
		{
			Name: "should return true when value present",
			Resolver: &mockResolver{
				value: []string{"someothervalue", expectedRecordValue},
				err:   nil,
			},
			ShouldExist: true,
		},
	}

	for _, tc := range cases {
		verify := dns.NewVerifier(tc.Resolver)
		t.Run(tc.Name, func(t *testing.T) {
			exists, err := verify.TxtRecordExists(context.TODO(), "example.com", expectedRecordValue)
			if tc.ExpectErr && err == nil {
				t.Fatalf("expected an err but got none")
			}
			if !tc.ExpectErr && err != nil {
				t.Fatalf("did not expect an err but got one %s", err)
			}
			if exists != tc.ShouldExist {
				t.Fatalf("did not expect the txt record to exist ")
			}

		})
	}
}
