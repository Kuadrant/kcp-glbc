# Sync Gateway API Resources

- Author: Phil Brookes <pbrookes@redhat.com>
- Epic: https://github.com/Kuadrant/kcp-glbc/issues/100
- Date: 23/05/2022

## Job Stories

- As an admin I want to be able to import the gateway API resources from an existing workspace so that I can create and sync gateway API resources to my workload clusters.
- As an admin I want to create a gateway in KCP that is synced to a workload cluster and activated so that I can pass the details to a developer to expose routes.
- As a developer I want to be able to read the gateway details in KCP so that I can create HTTPRoutes to expose my application to the internet.
- As a developer I want to easily discover the generated host on my httproute so that I can update the CNAME on my own domain name.

## Goals

- Create an API Export for gateway API resources (https://github.com/kcp-dev/kcp/issues/1083)
- Sync gateway API resources to workload clusters
- Have gateway API resources processed in the workload cluster
- Update / create CNAME for created HTTPRoutes
- Update HTTPRoute with created CNAME as host

## Non Goals

- cluster-wide gateway (https://github.com/istio/istio/issues/39031)
- installation of istio / istiod on workload cluster via glbc

## Proposed Solution

There are 3 main areas of work which will be expanded on below:
- Configuration of workload clusters
- Configuration of KCP
- Changes to the GLBC

### Workload clusters
Workload clusters will have the Gateway API CRDs and a networking CRD and the istio operator installed on them, and an istio control plane configured so that istiod is deployed. They will also need to allow the anyuid security constraint on all authorized users (see https://istio.io/latest/docs/setup/platform-setup/openshift/ for more info). 

#### Creating Gateway API CRDs
```
kubectl kustomize "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v0.4.0" | kubectl apply -f -
```

#### Creating NetworkAttachmentDefinition
Created from [this YAML](../../utils/kcp-contrib/gatewayapi/network-attachment-definition.crd.yaml)

#### installing Istio Operator
[Install istioctl](https://istio.io/latest/docs/ops/diagnostic-tools/istioctl/#install-hahahugoshortcode-s2-hbhb).
```
kubectl create namespace istio-system
istioctl operator init
kubectl kustomize "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v0.4.0" | kubectl apply -f -
kubectl  apply -f ./utils/kcp-contrib/gatewayapi/istio-controlplane-without-ingress.yaml
```
[istio control plane](../../utils/kcp-contrib/gatewayapi/istio-controlplane-without-ingress.yaml)

#### Permitting anyuid security constraint
```azure
oc adm policy add-scc-to-group anyuid system:authenticated
```

### KCP Cluster

#### Gateway API resources
A workspace will be created on the KCP cluster, which will contain the Gateway API resources and an API Export for them.
[Gateway API resources](../../utils/kcp-contrib/gatewayapi/gatewayapi-resources.yaml)
[Gateway API resources export](../../utils/kcp-contrib/gatewayapi/gatewayapi-export.yaml)

##### Example binding
[Gateway API resources binding](../../utils/kcp-contrib/gatewayapi/gatewayapi-binding.yaml)

#### Gateway API resource syncing
The syncer will be configured to sync Gateway API resources.
```azure
  ./bin/kubectl-kcp workload sync <cluster> --syncer-image=ghcr.io/kcp-dev/kcp/syncer:release-0.4 --resources=ingresses.networking.k8s.io,services,httproutes.gateway.networking.k8s.io,referencepolicies.gateway.networking.k8s.io,tcproutes.gateway.networking.k8s.io,tlsroutes.gateway.networking.k8s.io,udproutes.gateway.networking.k8s.io,gateways.gateway.networking.k8s.io,network-attachment-definitions.k8s.cni.cncf.io > ./tmp/<cluster>-syncer.yaml
```

### Controller updates

The GLBC will be coded to reconcile for HTTPRoutes and perform very similar tasks to ingress objects:

- Generate a host for hcpapps.net.
- Create / update a DNS record for that host.
- Apply the host to the httproute object.

The IP address(es) for the DNS record can be retrieved from a DNS Lookup on the hostname applied to the gateway CR.

## Workflow
As a customer with a KCP workspace active, I configure a workload cluster with the Istio operator, the gateway API CRDs and the NetworkAttachmentDefinition CRD.
In my KCP workspace I create an API binding to import the Gateway API CRDs into my workspace.
I create my application ([an example](../../samples/apigateway/echo.yaml)) using a Gateway and a HTTPRoute to expose it to the internet.
I wait for the HTTPRoute to have a hcpapps.net host added to it, and then confirm the application is responding on that host.
I optionally create my own hostname as a CNAME to the hcpapps.net host


## Testing

We should extend our existing tests around ingress objects to also cover use-cases with HTTPRoutes.

## Checklist

- [ ] An epic has been created and linked to
- [ ] Reviewers have been added. It is important that the right reviewers are selected. 