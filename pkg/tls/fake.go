/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tls

import "context"

type FakeProvider struct{}

var _ Provider = &FakeProvider{}

func (p *FakeProvider) IssuerID() string {
	return "fake"
}

func (p *FakeProvider) Domains() []string {
	return nil
}

func (p *FakeProvider) Create(_ context.Context, _ CertificateRequest) error {
	return nil
}
func (p *FakeProvider) Delete(_ context.Context, _ CertificateRequest) error {
	return nil
}

func (p *FakeProvider) Initialize(_ context.Context) error {
	return nil
}
