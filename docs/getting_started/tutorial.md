# GLBC

The GLBCs main API is the Kubernetes Ingress object. GLBC watches for ingress objects and mutates them adding in the GLBC managed host and TLS certificate.

For more information on the architecture of GLBC and how the various component work, refer to the [overview documentation](https://github.com/Kuadrant/kcp-glbc/blob/bb8e43639691568b594720244a0c94a23470a587/docs/getting_started/overview.md).

Use this tutorial to perform the following actions:

* Install the `kcp-glbc` instance and verify installation.
* Create a new ingress resource
* Interact with the workload cluster
* Troubleshoot installation 


## Installation

Clone the repo and run:

```bash
make local-setup
```
Note: If errors are being encountered during the local-setup, please refer to the "Troubleshooting Installation" section at the end of this document.

This script will: 
* build all the binaries
* Deploy three Kubernetes `1.22` clusters locally using kind.
* Deploy and configure the ingress controllers in each cluster.
* Start the KCP server.
* Create KCP workspaces for glbc and user resources:
    * kcp-glbc
    * kcp-glbc-compute
    * kcp-glbc-user
    * kcp-glbc-user-compute
* Add workload clusters to the `*-compute` workspaces
    * kcp-glbc-compute: 1x kind cluster
    * kcp-glbc-user-compute: 2x kind clusters
* Deploy glbc dependencies (cert-manager) into kcp-glbc workspace.

[comment]: <> (I can add here the 2 options that are displayed at the end of the local-setup and show their differences)

### Prerequisites
- Install Go 1.17 or higher as that is the version used in KCP-GLBC as per the [go.mod](https://github.com/Kuadrant/kcp-glbc/blob/main/go.mod) file.
- Have an AWS account, a DNS Zone, and a subdomain of the domain being used.

[comment]: <> (Will be adding more details to the AWS prerequisite as we gather more information)

###  Procedure

For more information refer to ...

### Verify Installation
[comment]: <> (I can show how to verify that kcp-glbc is running by deploying the sample service)

### Troubleshooting Installation
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
KUBECONFIG=config/deploy/local/kcp.kubeconfig ./bin/kubectl-kcp workspace use root:default:kcp-glbc-user-compute
Current workspace is "root:default:kcp-glbc-user-compute".
```
```bash
kubectl get workloadclusters -o wide
NAME            LOCATION        READY   SYNCED API RESOURCES
kcp-cluster-1   kcp-cluster-1   True    
kcp-cluster-2   kcp-cluster-2   False 
```
If a cluster is not in READY state, the following procedure might solve the issue: [Configure Linux for Many Watch Folders](https://www.ibm.com/docs/en/ahte/4.0?topic=wf-configuring-linux-many-watch-folders) (we want to bump up each of the limits).


## Providing ingress in a multi-cluster ingress scenario

A guide on how to use the GLBC to provide ingress in a multi-cluster ingress scenario (kind clusters are fine initially)


## Common Features 

### Testing

- Common features such as testing to see workloads moving after a workspace is unavailable






