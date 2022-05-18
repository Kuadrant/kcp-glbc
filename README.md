# KCP Global Load Balancer

![build status badge](https://github.com/kuadrant/kcp-glbc/actions/workflows/ci.yaml/badge.svg)

## Getting Started

Clone the repo and run:

```bash
make local-setup
```

This script will:

- build all the binaries
- deploy two kubernetes 1.22 clusters locally using `kind`.
- deploy and configure the ingress controllers in each cluster.
- start the KCP server.

Once the script is done, copy the `Run locally:` command from the output.
Open a new terminal, and from the root of the project, run the copied command.

Now you can create a new ingress resource from the root of the project:

```bash 
export KUBECONFIG=.kcp/admin.kubeconfig
kubectl create namespace default

# Single cluster
kubectl apply -n default -f samples/echo-service/echo.yaml
```

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

### Interacting directly with WorkloadClusters

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
# Start KCP GLBC
$ ./bin/kcp-glbc --kubeconfig .kcp/admin.kubeconfig --context admin --dns-provider fake --glbc-kubeconfig tmp/kcp-cluster-glbc-control.kubeconfig
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

                                                                                                                                                     
```
