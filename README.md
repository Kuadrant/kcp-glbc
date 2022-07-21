# KCP Global Load Balancer

![build status badge](https://github.com/kuadrant/kcp-glbc/actions/workflows/ci.yaml/badge.svg)

The KCP Global Load Balancer Controller (GLBC) solves multi cluster ingress use cases when leveraging KCP to provide transparent multi cluster deployments. 

The main use case it solves currently is providing you with a single host that can be used to access your workload and bring traffic to the correct physical clusters. The GLBC manages the DNS for this host and provides you with a valid TLS certificate. If your workload moves/is moved or expands contracts across clusters, GLBC will ensure that the DNS for this host is correct and traffic will continue to reach your workload.

It also offers the ability to setup a health check for your workload. When this health check fails for a particular cluster, the unhealthy cluster will be removed from the DNS response.

The GLBCs main API is the kubernetes Ingress object. GLBC watches for Ingress objects and mutates them adding in the GLBC managed host and tls certificate. 

In the future we will also add the same functionality for Gateway API and OpenShift Route. 

## Getting Started

Clone the repo and run:

```bash
make local-setup
```

This script will:

- build all the binaries
- deploy three kubernetes 1.22 clusters locally using `kind`.
- deploy and configure the ingress controllers in each cluster.
- start the KCP server.
- create KCP workspaces for glbc and user resources:
  - kcp-glbc
  - kcp-glbc-compute
  - kcp-glbc-user
  - kcp-glbc-user-compute
- add workload clusters to the *-compute workspaces
  - kcp-glbc-compute: 1x  kind cluster
  - kcp-glbc-user-compute: 2x kind clusters
- deploy glbc dependencies (cert-manager) into kcp-glbc workspace.
    

Once the script is done, copy the `Run locally:` commands from the output.
Open a new terminal, and from the root of the project, run the copied commands.

Now you can create a new ingress resource from the root of the project:

```bash 
export KUBECONFIG=.kcp/admin.kubeconfig
./bin/kubectl-kcp workspace use root:default:kcp-glbc-user
kubectl apply -f samples/echo-service/echo.yaml
```
N.B. It's important that you use the `.kcp/admin.kubeconfig` kube config and switch to the `root:default:kcp-glbc-user` workspace.

To verify the resources were created successfully, check the output of the following:

```bash
kubectl get deployment,service,ingress
```

### Add CRC cluster (optional)

With a running local setup i.e. you have successfully executed `make local-setup`, you can run the following to create and add a CRC cluster:

```bash
./utils/local-setup-add-crc-cluster.sh
```

```bash
$ kubectl get workloadclusters -o wide
NAME              LOCATION          READY   SYNCED API RESOURCES
kcp-cluster-1     kcp-cluster-1     True    ["deployments.apps","ingresses.networking.k8s.io","secrets","services"]
kcp-cluster-2     kcp-cluster-2     True    ["deployments.apps","ingresses.networking.k8s.io","secrets","services"]
kcp-cluster-crc   kcp-cluster-crc   True    ["deployments.apps","ingresses.networking.k8s.io","secrets","services"]
```

You must have crc installed(https://crc.dev/crc/), and have your openshift pull secret(https://cloud.redhat.com/openshift/create/local) stored locally in `~/pull-secret`. 
Please check the script comments for any version requirements.

## Development

###Interacting directly with WorkloadClusters

Prefix `kubectl` with the appropriate kubeconfig file from the tmp directory.
For example:

```bash
KUBECONFIG=./tmp/kcp-cluster-1.kubeconfig kubectl get deployments,services,ingress --all-namespaces
```

### Testing

The e2e tests can be executed locally by running the following commands:

```bash
# Start KCP and the KinD clusters
$ make local-setup
export KUBECONFIG=config/deploy/local/kcp.kubeconfig
./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc
export CLUSTERS_KUBECONFIG_DIR=$(pwd)/tmp
export AWS_DNS_PUBLIC_ZONE_ID=YOUR_ZONE_ID
./bin/kubectl-kcp workspace use root:default:kcp-glbc
# Start KCP GLBC
$ ./bin/kcp-glbc --kubeconfig .kcp/admin.kubeconfig --context system:admin --dns-provider fake
# Run the e2e test suite
$ make e2e
```

Alternatively, You can run the KCP GLBC and/or the tests from your IDE / debugger.

### Local development on a mac
MacOS does not default to allowing you to communicate with ports running in docker containers from the host machine. To bypass this limitation we can use `docker-mac-net-connect`:
```
# Install via Homebrew
$ brew install chipmk/tap/docker-mac-net-connect

# Run the service and register it to launch at boot
$ sudo brew services start chipmk/tap/docker-mac-net-connect
```

This is done automatically as part of the `make local-setup` but will require presenting a sudo password to start the service if it is not configured to autostart.

N.B. This does not remove the requirement to have the DNS records created in a valid DNS service (e.g. route53 in AWS).

## Troubleshooting

### "make local-setup" command.
While attempting to run make local-setup it’s possible you will encounter some of the following errors:
<br><br>

**make: *** No rule to make target 'local-setup':**
After cloning the repo, make sure to run the “make local-setup” command in the directory where the repo was cloned.<br><br>


**bash: line 1: go: command not found:**
We must install the correct go version used for this project. The version number can be found in the go.mod file of the repo. In this case, it is go 1.17.
If running on Fedora, here is a [guide to install go on Fedora 36](https://nextgentips.com/2022/05/21/how-to-install-go-1-18-on-fedora-36/). Before running the command to install go, make sure to type in the correct go version that is needed.<br><br>


**kubectl: command not found:**
Here is a quick and easy way of [installing kubectl on Fedora](https://snapcraft.io/install/kubectl/fedora).<br><br>


**Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?:**
Run the following command to start Docker daemon:
```bash
sudo systemctl start docker
```
<br><br>
**Kind cluster failed to become ready - Check logs for errors:**
Attempt the following to confirm if *kcp-cluster-1* and *kcp-cluster-2* are in a READY state:
```bash
KUBECONFIG=config/deploy/local/kcp.kubeconfig ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc-user-compute
Current workspace is "root:users:ev:vo:system-apiserver:kcp-glbc-user-compute".
```
```bash
kubectl get workloadclusters -o wide
NAME            LOCATION        READY   SYNCED API RESOURCES
kcp-cluster-1   kcp-cluster-1   True    
kcp-cluster-2   kcp-cluster-2   False 
```
If a cluster is not in READY state, the following procedure might solve the issue: [Configure Linux for Many Watch Folders](https://www.ibm.com/docs/en/ahte/4.0?topic=wf-configuring-linux-many-watch-folders) (we want to bump up each of the limits).
