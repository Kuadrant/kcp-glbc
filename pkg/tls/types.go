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

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const tlsIssuerAnnotation = "kuadrant.dev/tls-issuer"

type Provider interface {
	IssuerID() string
	Domains() []string
	Create(ctx context.Context, cr CertificateRequest) error
	Delete(ctx context.Context, cr CertificateRequest) error
	Initialize(ctx context.Context) error
}

type CertificateRequest interface {
	Name() string
	CreationTimestamp() metav1.Time
	Labels() map[string]string
	Annotations() map[string]string
	Host() string
}
