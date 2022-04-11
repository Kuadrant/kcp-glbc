#!/bin/bash

#
# Copyright 2021 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

usage() { echo "usage: ./local-setup.sh -c <number of clusters>" 1>&2; exit 1; }
while getopts ":c:" arg; do
  case "${arg}" in
    c)
      NUM_CLUSTERS=${OPTARG}
      ;;
    *)
      usage
      ;;
  esac
done
shift $((OPTIND-1))


if [ -z "${NUM_CLUSTERS}" ]; then
    usage
fi

set -e pipefail

trap cleanup EXIT 1 2 3 6 15

cleanup() {
  echo "Killing KCP"
  kill "$KCP_PID"
}

GOROOT=$(go env GOROOT)
export GOROOT
export KIND_BIN="./bin/kind"
export KCP_BIN="./bin/kcp"
export KUBECTL_KCP_BIN="./bin/kubectl-kcp"
TEMP_DIR="./tmp"
KCP_LOG_FILE="${TEMP_DIR}"/kcp.log

KIND_CLUSTER_PREFIX="kcp-cluster-"
KCP_GLBC_CLUSTER_NAME="${KIND_CLUSTER_PREFIX}glbc-control"
KCP_GLBC_KUBECONFIG="${KCP_GLBC_CLUSTER_NAME}.kubeconfig"

for ((i=1;i<=$NUM_CLUSTERS;i++))
do
	CLUSTERS="${CLUSTERS}${KIND_CLUSTER_PREFIX}${i} "
done

mkdir -p ${TEMP_DIR}

createGLBCCluster() {
  ${KIND_BIN} create cluster --name ${KCP_GLBC_CLUSTER_NAME}
  ${KIND_BIN} get kubeconfig --name=${KCP_GLBC_CLUSTER_NAME} > ${TEMP_DIR}/${KCP_GLBC_KUBECONFIG}
  ${KIND_BIN} get kubeconfig --internal --name=${KCP_GLBC_CLUSTER_NAME} > ${TEMP_DIR}/${KCP_GLBC_KUBECONFIG}.internal

  echo "Deploying cert manager to kind glbc cluster"
  kubectl --context kind-${KCP_GLBC_CLUSTER_NAME} apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.7.1/cert-manager.yaml

  kubectl --context kind-${KCP_GLBC_CLUSTER_NAME} -n cert-manager wait --timeout=300s --for=condition=Available deployments --all
}

createCluster() {
  cluster=$1;
  port80=$2;
  port443=$3;
  cat <<EOF | ${KIND_BIN} create cluster --name ${cluster} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:v1.22.7@sha256:1dfd72d193bf7da64765fd2f2898f78663b9ba366c2aa74be1fd7498a1873166
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: ${port80}
    protocol: TCP
  - containerPort: 443
    hostPort: ${port443}
    protocol: TCP
EOF

  ${KIND_BIN} get kubeconfig --name=${cluster} > ${TEMP_DIR}/${cluster}.kubeconfig
  ${KIND_BIN} get kubeconfig --internal --name=${cluster} > ${TEMP_DIR}/${cluster}.kubeconfig.internal

  echo "Creating Cluster objects for the kind cluster."
  ${KIND_BIN} get kubeconfig --name=${cluster} | sed -e 's/^/    /' | cat utils/kcp-contrib/cluster.yaml - | sed -e "s/name: local/name: ${cluster}/" > ${TEMP_DIR}/${cluster}.yaml

  echo "Deploying Ingress controller to kind cluster"
  {
  kubectl config use-context kind-${cluster}

  VERSION=$(curl https://raw.githubusercontent.com/kubernetes/ingress-nginx/master/stable.txt)
  curl https://raw.githubusercontent.com/kubernetes/ingress-nginx/"${VERSION}"/deploy/static/provider/kind/deploy.yaml | sed "s/--publish-status-address=localhost/--report-node-internal-ip-address/g" | kubectl apply -f -
  kubectl annotate ingressclass nginx "ingressclass.kubernetes.io/is-default-class=true"

  } &>/dev/null
}

clusterCount=$(${KIND_BIN} get clusters | grep ${KIND_CLUSTER_PREFIX} | wc -l)
if ! [[ $clusterCount =~ "0" ]] ; then
  echo "Deleting previous kind clusters."
  ${KIND_BIN} get clusters | grep ${KIND_CLUSTER_PREFIX} | xargs ${KIND_BIN} delete clusters
fi

echo "Deploying 1 kind k8s glbc cluster locally."

createGLBCCluster

echo "Deploying $NUM_CLUSTERS kind k8s clusters locally."

port80=8080
port443=8443
for cluster in $CLUSTERS
do
  createCluster "$cluster" $port80 $port443
  port80=$((port80+1))
  port443=$((port443+1))
#move to next cluster
done

echo "Starting KCP, sending logs to ${KCP_LOG_FILE}"
${KCP_BIN} start --push-mode --discovery-poll-interval 3s --run-controllers --resources-to-sync=secrets,deployments,services,ingresses.networking.k8s.io --auto-publish-apis > ${KCP_LOG_FILE} 2>&1 &
KCP_PID=$!

echo "Waiting 15 seconds..."
sleep 15

if ! ps -p ${KCP_PID}; then
  echo "####"
  echo "---> KCP failed to start, see ${KCP_LOG_FILE} for info."
  echo "####"
  exit 1 #this will trigger cleanup function
fi

echo "Exporting KUBECONFIG=.kcp/admin.kubeconfig"
export KUBECONFIG=.kcp/admin.kubeconfig

echo "Creating HCG Workspace"
${KUBECTL_KCP_BIN} workspace create kcp-glbc --enter

echo "Registering HCG APIs"
kubectl apply -f ./config/crd/bases
kubectl apply -f ./utils/kcp-contrib/apiresourceschema.yaml
kubectl apply -f ./utils/kcp-contrib/apiexport.yaml

echo "Registering kind k8s clusters into KCP"
kubectl apply $(find ./tmp/kcp-cluster-*.yaml | awk ' { print " -f " $1 } ')

echo ""
echo "KCP PID          : ${KCP_PID}"
echo ""
echo "The kind k8s clusters have been registered, and KCP is running, now you should run the kcp-ingress"
echo "example: "
echo ""
echo "       ./bin/kcp-glbc --kubeconfig .kcp/admin.kubeconfig --context system:admin --glbc-kubeconfig ${TEMP_DIR}/${KCP_GLBC_KUBECONFIG}"
echo ""
echo "Don't forget to export the proper KUBECONFIG to create objects against KCP:"
echo "export KUBECONFIG=${PWD}/.kcp/admin.kubeconfig"
echo ""
read -p "Press enter to exit -> It will kill the KCP process running in background"
