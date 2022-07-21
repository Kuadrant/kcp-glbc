## Local Development

The following describes how to deploy and test the GLBC running on a kind cluster created as part of the local development setup.

### Terminal 1

#### Run KCP

Run the `local-setup` script to create the test kind clusters, configure KCP workspaces and start a local KCP process.

```shell
$ make local-setup
```

### Terminal 2

Target the local deployment kcp kubeconfig:

```shell
export KUBECONFIG=config/deploy/local/kcp.kubeconfig
```

#### Check local setup state

##### Local deploy config

If this is the first time running the `make local-setup` command it will generate a set of local configuration files which
can be used to modify the configuration of GLBC.

```shell
$ tree -I '*.yaml|*.template' config/deploy/local/
config/deploy/local/
├── aws-credentials.env
├── controller-config.env
└── kcp.kubeconfig
```

These files will not be committed and can be modified as required by you.

##### Workspaces

The following workspaces are created:

```shell
$ ./bin/kubectl-kcp workspace
Current workspace is "root:users:ev:vo:system-apiserver".
$ ./bin/kubectl-kcp workspace list
NAME                    TYPE        PHASE   URL
kcp-glbc                Universal   Ready   https://192.168.0.103:6443/clusters/root:users:ev:vo:system-apiserver:kcp-glbc
kcp-glbc-compute        Universal   Ready   https://192.168.0.103:6443/clusters/root:users:ev:vo:system-apiserver:kcp-glbc-compute
kcp-glbc-user           Universal   Ready   https://192.168.0.103:6443/clusters/root:users:ev:vo:system-apiserver:kcp-glbc-user
kcp-glbc-user-compute   Universal   Ready   https://192.168.0.103:6443/clusters/root:users:ev:vo:system-apiserver:kcp-glbc-user-compute
```

**kcp-glbc**

```shell
$ ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc
Current workspace is "root:users:ev:vo:system-apiserver:kcp-glbc".
$ kubectl get apiexports
NAME                  AGE
cert-manager-stable   16m
glbc                  16m
$ kubectl get apibindings
NAME           AGE
cert-manager   16m
glbc           16m
kubernetes     16m
$ kubectl get deployments --all-namespaces
NAMESPACE      NAME           READY   UP-TO-DATE   AVAILABLE   AGE
cert-manager   cert-manager   1/1     1            1           15m
```

**kcp-glbc-compute**

```shell
$ ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc-compute
Current workspace is "root:users:ev:vo:system-apiserver:kcp-glbc-compute".
$ kubectl get workloadclusters -o wide
NAME   LOCATION   READY   SYNCED API RESOURCES
glbc   glbc       True    
$ kubectl get apiexports
NAME         AGE
kubernetes   14m
$ kubectl get apibindings
NAME         AGE
kubernetes   14m
$ kubectl get negotiatedapiresources
NAME
deployments.v1.apps
ingresses.v1.networking.k8s.io
services.v1.core
```

**kcp-glbc-user**

```shell
$ ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc-user
Current workspace is "root:users:ev:vo:system-apiserver:kcp-glbc-user".
$ kubectl get apiexports
No resources found
$ kubectl get apibindings
NAME         AGE
glbc         20m
kubernetes   20m
$ kubectl get deployments --all-namespaces
No resources found
```

**kcp-glbc-user-compute**

```shell
$ ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc-user-compute
Current workspace is "root:users:ev:vo:system-apiserver:kcp-glbc-user-compute".
$ kubectl get workloadclusters -o wide
NAME            LOCATION        READY   SYNCED API RESOURCES
kcp-cluster-1   kcp-cluster-1   True    
kcp-cluster-2   kcp-cluster-2   True    
$ kubectl get apiexports
NAME         AGE
kubernetes   16m
$ kubectl get apibindings
NAME         AGE
kubernetes   16m
$ kubectl get negotiatedapiresources
NAME
deployments.v1.apps
ingresses.v1.networking.k8s.io
services.v1.core
```

#### Deploy GLBC

Deploy the GLBC into the `kcp-glbc` workspace.

```shell
$ ./utils/deploy.sh
```

Check the deployment:
```shell
$ ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc
$ kubectl get deployments --all-namespaces
NAMESPACE      NAME                          READY   UP-TO-DATE   AVAILABLE   AGE
cert-manager   cert-manager                  1/1     1            1           30m
kcp-glbc       kcp-glbc-controller-manager   1/1     1            1           11s
```

It's not currently possible to check the logs via KCP, but you can view them by accessing the deployment on the kind cluster directly: 

```shell
$ kubectl --kubeconfig ~/.kube/config --context kind-kcp-cluster-glbc-control logs -f deployments/kcp-glbc-controller-manager -n kcp89b5fd4ba9405ee7b18d0da859ce7420d36926bac4a97e01af5c244a               
2022-06-20T09:59:47.622Z INFO runtime/proc.go:255 Creating TLS certificate provider {"issuer": "glbc-ca"}
....
```
N.B. Make sure you run this command using the correct kubeconfig and context i.e. Not the KCP one.

### Terminal 3

Test the deployment using the sample service.

```shell
$ export KUBECONFIG=.kcp/admin.kubeconfig
$ ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc-user
Current workspace is "root:users:ev:vo:system-apiserver:kcp-glbc-user".
$ kubectl apply -f samples/echo-service/echo.yaml
service/httpecho-both created
deployment.apps/echo-deployment created
ingress.networking.k8s.io/ingress-nondomain created
```

Check the deployment:
```shell
$ kubectl get deployments --all-namespaces
NAMESPACE   NAME              READY   UP-TO-DATE   AVAILABLE   AGE
default     echo-deployment   1/1     1            1           48s
```

Check what cluster was selected for the deployment:

```shell
$ kubectl get ns default -o json | jq .metadata.annotations
{
  "internal.scheduling.kcp.dev/negotiation-workspace": "root:users:ev:vo:system-apiserver:kcp-glbc-user-compute",
  "scheduling.kcp.dev/placement": "{\"root:users:ev:vo:system-apiserver:kcp-glbc-user-compute+location-1+kcp-cluster-1\":\"Pending\"}"
}
$ kubectl get ns default -o json | jq .metadata.labels
{
  "kubernetes.io/metadata.name": "default",
  "state.internal.workload.kcp.dev/kcp-cluster-1": "Sync"
}
```

Check the logs:

```shell
$ kubectl --kubeconfig ~/.kube/config --context kind-kcp-cluster-1 get deployments --all-namespaces | grep echo
kcp02ae827aeeb1782e217a0044068ee76384484349fc4a75242b133c52       echo-deployment            1/1     1            1           6m35s
```

```shell
$ kubectl --kubeconfig ~/.kube/config --context kind-kcp-cluster-1 logs -f deployments/echo-deployment -n kcp02ae827aeeb1782e217a0044068ee76384484349fc4a75242b133c52
Echo server listening on port 8080.
```
N.B. Make sure you run this command using the correct kubeconfig i.e., Not the KCP one, and set the context to the selected cluster.
