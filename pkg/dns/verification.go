package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type resolver interface {
	LookupTXT(ctx context.Context, domain string) ([]string, error)
}

func NewVerifier(resolver resolver) *verifier {
	return &verifier{
		resolver: resolver,
	}
}

type verifier struct {
	resolver resolver
}

type VerificationError struct {
	msg string
	err error
}

func IsNoSuchHost(err error) bool {
	var vError *VerificationError
	ok := false
	if vError, ok = err.(*VerificationError); !ok {
		return false
	}

	var dnsError *net.DNSError
	if dnsError, ok = vError.err.(*net.DNSError); !ok {
		return false
	}

	return dnsError.Err == "no such host"
}

func (err *VerificationError) Error() string {
	return fmt.Sprintf("%s: %v", err.msg, err.err)
}

func newVerificationError(msg string, err error) *VerificationError {
	return &VerificationError{
		msg,
		err,
	}
}

var _ error = &VerificationError{}

func (v *verifier) TxtRecordExists(ctx context.Context, domain, value string) (bool, error) {
	values, err := v.resolver.LookupTXT(ctx, domain)
	if err != nil {
		return false, newVerificationError(fmt.Sprintf("error looking for TXT record on '%v'", domain), err)
	}

	for _, txtValue := range values {
		if strings.TrimSpace(txtValue) == strings.TrimSpace(value) {
			return true, nil
		}
	}
	return false, nil
}
