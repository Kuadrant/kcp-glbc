# Deployment

This document describes the process of deploying GLBC using the `./utils/deploy.sh` script directly. 
This requires you to have access to a KCP instance to deploy GLBC to, if you want to try it using a locally running KCP instance and KinD clusters, please refer to the [`local deployment`](local_deployment.md) docs.

**Prerequisite**

* KCP version 0.5

**Useful Links**

* [KCP](https://github.com/kcp-dev/kcp)
* [KCP Docs](https://github.com/kcp-dev/kcp/blob/main/docs)
* [KCP Locations and Scheduling](https://github.com/kcp-dev/kcp/blob/main/docs/locations-and-scheduling.md#locations-and-scheduling)

## GLBC

Deploy the GLBC using a default configuration (config/deploy/local).

```
./utils/deploy.sh
```
N.B. You must be targeting a KCP instance in order to install GLBC.

### Workspaces

After a successful deployment you will have the following workspaces in your target KCP:

```
./bin/kubectl-kcp workspace use root:<your org>
Current workspace is "root:<your org>". $ ./bin/kubectl-kcp workspace list
NAME                    TYPE        PHASE   URL
kcp-glbc                Universal   Ready   https://192.168.0.103:6443/clusters/root:<your org>:kcp-glbc
kcp-glbc-compute        Universal   Ready   https://192.168.0.103:6443/clusters/root:<your org>:kcp-glbc-compute
kcp-glbc-user           Universal   Ready   https://192.168.0.103:6443/clusters/root:<your org>:kcp-glbc-user
kcp-glbc-user-compute   Universal   Ready   https://192.168.0.103:6443/clusters/root:<your org>:kcp-glbc-user-compute 
```

| Workspace | Description |
| ---------- | ----------- |
| `kcp-glbc` |  Workspace containing GLBC, and it's dependencies (cert-manager), deployments. Exposes the `glbc` APIExport that user workspaces bind to, and binds to the `kubernetes` APIExport from `kcp-glbc-compute` for compute and locations. |
| `kcp-glbc-compute` |  Compute service workspace providing compute specifically for GLBC components. Exposes the `kubernetes` APIExport that the `kcp-glbc` workspace binds to, and has 1 Location by default. |
| `kcp-glbc-user` |  Workspace created for user application deployments e.g. sample echo service. Binds to the glbc APIExport from `kcp-glbc`, and the, `kubernetes` APIExport from `kcp-glbc-user-compute` for compute and locations. |
| `kcp-glbc-user-compute` |  Compute service workspace providing compute specifically for user deployments. Exposes the `kubernetes` APIExport that the `kcp-glbc-user` workspace binds to, and has 1 Location by default. |

### Workload Cluster

The deployment script will create a `glbc` workload cluster in the `kcp-glbc-compute` workspace, however, for GLBC and it's dependencies to deploy the [`KCP syncer`](https://github.com/kcp-dev/kcp/blob/main/docs/syncer.md) resources must be applied to a physical cluster.
The exact command will be output as part of the deploy script, but will look something like:

```
# targeting your physical cluster run:

kubectl apply -f ./config/deploy/local/glbc-syncer.yaml
```

Verify the workload cluster becomes ready:
```
# targeting your kcp cluster run:

./bin/kubectl-kcp workspace use root:<your org>:kcp-glbc-compute
kubectl get workloadclusters -o wide
NAME   LOCATION   READY   SYNCED API RESOURCES
glbc   glbc       True
```
N.B. It can take a couple of minutes for it go into a "ready" state.

Verify GLBC deployments:
```
./bin/kubectl-kcp workspace use root:<your org>:kcp-glbc
kubectl get deployments --all-namespaces
NAMESPACE      NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
cert-manager   cert-manager                  1/1     1            1           38m
kcp-glbc       kcp-glbc-controller-manager   1/1     1            1           37m
```

### User Workload Cluster

A separate user workspace `kcp-glbc-user` is created by default which has it's own compute service workspace pre configured.
Like the `glbc` workload cluster, it also requires a physical cluster to have the [`KCP syncer`](https://github.com/kcp-dev/kcp/blob/main/docs/syncer.md) resources applied.
The exact command will be output as part of the deploy script, but will look something like:

```
# targeting your physical cluster run:

kubectl apply -f ./config/deploy/local/glbc-user-syncer.yaml
```

Verify it's ready:
```
# targeting your kcp cluster run:

./bin/kubectl-kcp workspace use root:<your org>:kcp-glbc-user-compute
kubectl get workloadclusters -o wide
NAME        LOCATION        READY   SYNCED API RESOURCES
glbc-user   glbc-user       True
```

N.B. This can be the same workload cluster used for the `glbc` in the step above.

## Configuration

### KCP Kubeconfig (Required)

A secret `secret/kcp-glbc-kcp-kubeconfig` containing the KCP cluster kubeconfig. An empty secret is created by default 
during installation, but can be replaced with:  

```
kubectl -n kcp-glbc create secret generic kcp-glbc-kcp-kubeconfig --from-file=kubeconfig=$(KCP_KUBECONFIG)
```

### AWS Credentials (Optional) 

A secret  `secret/kcp-glbc-aws-credentials` containing AWS access key and secret. This is only required if `GLBC_DNS_PROVIDER` is set to `aws`.
The credentials must have permissions to create/update/delete records in the hosted zone set in `AWS_DNS_PUBLIC_ZONE_ID`, and the
domain set in `GLBC_DOMAIN` corresponds to the public zone id. An empty secret is created by default during installation, 
but can be replaced with:

```
kubectl -n kcp-glbc create secret generic kcp-glbc-aws-credentials \
--from-literal=AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID} \
--from-literal=AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
```

### TLS Issuer provider (Optional) 

A TLS Issuer provider supported by cert-manager and created via KCP before running the GLBC controller is required only if the genaration of TLS certs (GLBC_TLS_PROVIDED) for the GLBC is enabled. 

A reference to the TLS certificate issuer resource can be passed when starting the GLBC using the tag `--glbc-tls-provider` or the environment variables `GLBC_TLS_PROVIDER`

Refer to the [cert-manager repo](https://github.com/cert-manager/cert-manager#cert-manager) to learn more about the supported providers and how to create a cert issuer.

There is also a script that generates a let's encrypt issuer against KCP that can be triggered using the command below:

```
go run ./utils/certman-issuer/ 
--glbc-kubeconfig <Path to GLBC kubeconfig> 
--glbc-tls-provider <The TLS certificate issuer, one of [glbc-ca, le-staging, le-production]> 
--region <the region we should target with AWS clients>
--issuer-namespace <namespace where the issuer resource will be created, the namespace should match with the namespace where the glbc is deployed>
```

### GLBC Controller Options (Optional)

A config map `configmap/kcp-glbc-controller-config` containing GLBC configuration options. A config map is created by 
default during installation containing the default values, but can be replaced by editing the config map:

```
kubectl -n kcp-glbc edit configmap kcp-glbc-controller-config
```

| Annotation | Description | Default value |
| ---------- | ----------- | ------------- |
| `AWS_DNS_PUBLIC_ZONE_ID` |  AWS hosted zone id where route53 records will be created (default is dev.hcpapps.net) | Z08652651232L9P84LRSB |
| `GLBC_DNS_PROVIDER` |  The dns provider to use, one of [aws, fake] | fake |
| `GLBC_DOMAIN` |  The domain to use when exposing ingresses via glbc | dev.hcpapps.net |
| `GLBC_ENABLE_CUSTOM_HOSTS` | Allow custom hosts in glbc managed ingresses | false |
| `GLBC_KCP_CONTEXT` | The kcp kube context | system:admin |
| `GLBC_LOGICAL_CLUSTER_TARGET` | logical cluster to target | `*` |
| `GLBC_TLS_PROVIDED` | Generate TLS certs for glbc managed hosts | false |
| `GLBC_TLS_PROVIDER` | The TLS certificate issuer | glbc-ca |
| `HCG_LE_EMAIL` | Email address to use during LE cert requests | kuadrant-dev@redhat.com |
| `NAMESPACE` | Target namesapce of rcert-manager resources (issuers, certificates) | kcp-glbc |
| `GLBC_WORKSPACE` | The GLBC workspace| root:users:ev:vo:system-apiserver:kcp-glbc |
| `GLBC_COMPUTE_WORKSPACE` | The user compute workspace | root:users:ev:vo:system-apiserver:kcp-glbc-user-compute |

### Applying configuration changes

Any of the described configurations can be modified after the initial creation of the resources, the deploymnet will however 
need to be restarted after each change in order for them to come into affect.

`kubectl rollout restart deployment/kcp-glbc-controller-manager -n kcp-glbc`


## Configuring for remote KCP

If you are not using a local KCP, you will need to create a kubeconfig that allows GLBC to connect to the remote KCP instance. 

To do this, use the following steps. (note you will need the KCP kube plugin which is part of the kcp repo)

1) Login to KCP and select your workspace

```
kubectl kcp workspace my-workspace
```

2) Create a service account

```
kubectl create sa glbc
```

3) create a cluster role and bind it to your service account

```
kubectl create -f config/kcp/glbc-cluster-role.yaml

kubectl create -f config/kcp/glbc-cluster-role-binding.yaml

```

4) extract the service account token

```
kubectl get secrets

$(kubectl --namespace default get secret/glbc-token-<id> -o jsonpath='{.data.token}' | base64 --decode)
```

5) copy the token into a kubeconfig. An example template has been added to ```config/kcp/kcp-cube-config.yaml.template ```


You can now run GLBC targeting the KCP instance by passing this kubeconfig file as a start up parameter

```
--kubeconfig=<path to kcp kube config>
```

Note: When targeting a remote KCP you may not have access to all workspaces. If that's the case you will need to start the controller with the logical cluster specified `--logical-cluster root:<my org>:<my workspace>`  


