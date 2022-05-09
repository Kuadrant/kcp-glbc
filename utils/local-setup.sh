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

DO_BREW="false"
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

usage() { echo "usage: ./local-setup.sh -c <number of clusters>" 1>&2; exit 1; }
while getopts ":bc:" arg; do
  case "${arg}" in
    c)
      NUM_CLUSTERS=${OPTARG}
      ;;
    b)
      DO_BREW="true"
      ;;
    *)
      usage
      ;;
  esac
done
shift $((OPTIND-1))

if [[ "$DO_BREW" == "true" ]]; then
  if [[ "${OSTYPE}" =~ ^darwin.* ]]; then
    ${SCRIPT_DIR}/macos/required_brew_packages.sh
  fi
else
  echo "skipping brew"
fi

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
export KUSTOMIZE_BIN="./bin/kustomize"
TEMP_DIR="./tmp"
KCP_LOG_FILE="${TEMP_DIR}"/kcp.log

KIND_CLUSTER_PREFIX="kcp-cluster-"
KCP_GLBC_CLUSTER_NAME="${KIND_CLUSTER_PREFIX}glbc-control"
KCP_GLBC_KUBECONFIG="${KCP_GLBC_CLUSTER_NAME}.kubeconfig"

: ${KCP_VERSION:="release-0.4"}
KCP_SYNCER_IMAGE="ghcr.io/kcp-dev/kcp/syncer:${KCP_VERSION}"

for ((i=1;i<=$NUM_CLUSTERS;i++))
do
	CLUSTERS="${CLUSTERS}${KIND_CLUSTER_PREFIX}${i} "
done

mkdir -p ${TEMP_DIR}

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

  echo "Deploying Ingress controller to kind cluster"
  {
  kubectl config use-context kind-${cluster}

  VERSION=$(curl https://raw.githubusercontent.com/kubernetes/ingress-nginx/master/stable.txt)
  curl https://raw.githubusercontent.com/kubernetes/ingress-nginx/"${VERSION}"/deploy/static/provider/kind/deploy.yaml | sed "s/--publish-status-address=localhost/--report-node-internal-ip-address/g" | kubectl apply -f -
  kubectl annotate ingressclass nginx "ingressclass.kubernetes.io/is-default-class=true"
  } &> /dev/null

}

createKCPWorkloadCluster() {
  createCluster $1 $2 $3
  resourcesToSync="ingresses.networking.k8s.io,services,gateways.gateway.networking.k8s.io,gateways.networking.istio.io,httproutes.gateway.networking.k8s.io,referencepolicies.gateway.networking.k8s.io,serviceentries.networking.istio.io,tcproutes.gateway.networking.k8s.io,tlsroutes.gateway.networking.k8s.io,udproutes.gateway.networking.k8s.io"
  echo "Deploying kcp syncer to ${1}"
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workload sync $1 --syncer-image=${KCP_SYNCER_IMAGE} --resources=${resourcesToSync} > ${TEMP_DIR}/${1}-syncer.yaml
  kubectl config use-context kind-${1}
  kubectl apply -f ${TEMP_DIR}/${1}-syncer.yaml

  # install istio-operator and istiod
  kubectl create namespace istio-system
  istioctl operator init
  kubectl kustomize "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v0.4.0" | kubectl apply -f -
  kubectl  apply -f ${SCRIPT_DIR}/kcp-contrib/gatewayapi/istio-controlplane-without-ingress.yaml

  # connect istio gateway to Gateway API
#  kubectl apply -f ${SCRIPT_DIR}/kcp-contrib/gatewayapi/gatewayapi.yaml
}

createGLBCCluster() {
  createCluster "$KCP_GLBC_CLUSTER_NAME" $1 $2
  kubectl config use-context kind-${KCP_GLBC_CLUSTER_NAME}
  echo "Deploying cert manager to ${KCP_GLBC_CLUSTER_NAME}"
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.7.1/cert-manager.yaml
  kubectl -n cert-manager wait --timeout=300s --for=condition=Available deployments --all
}

createKCPWorkspace() {
  echo "Creating HCG Workspace"
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workspace create kcp-glbc --enter
  echo "Waiting 15 seconds..."
  sleep 15
  echo "Registering HCG APIs"
  kubectl --kubeconfig=.kcp/admin.kubeconfig apply -f ./config/crd/bases
  kubectl --kubeconfig=.kcp/admin.kubeconfig apply -f ./utils/kcp-contrib/apiresourceschema.yaml
  kubectl --kubeconfig=.kcp/admin.kubeconfig apply -f ./utils/kcp-contrib/apiexport.yaml

  echo "creating apigateway workspace"
  # create apigateway workspace and CRDs
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workspace root:default
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workspace create apigateway --enter
  echo "creating apigateway resources"
  kubectl --kubeconfig=.kcp/admin.kubeconfig create -f ${SCRIPT_DIR}/kcp-contrib/gatewayapi/gatewayapi-resources.yaml
  echo "creating apigateway resources export"
  kubectl --kubeconfig=.kcp/admin.kubeconfig create -f ${SCRIPT_DIR}/kcp-contrib/gatewayapi/gatewayapi-export.yaml

  #import gateway APIs into default workspace
  KUBECONFIG=.kcp/admin.kubeconfig ${KUBECTL_KCP_BIN} workspace root:default:kcp-glbc
  kubectl --kubeconfig=.kcp/admin.kubeconfig create -f ${SCRIPT_DIR}/kcp-contrib/gatewayapi/gatewayapi-binding.yaml
}

#Delete existing kind clusters
clusterCount=$(${KIND_BIN} get clusters | grep ${KIND_CLUSTER_PREFIX} | wc -l)
if ! [[ $clusterCount =~ "0" ]] ; then
  echo "Deleting previous kind clusters."
  ${KIND_BIN} get clusters | grep ${KIND_CLUSTER_PREFIX} | xargs ${KIND_BIN} delete clusters
fi

#1. Start KCP
echo "Starting KCP, sending logs to ${KCP_LOG_FILE}"
${KCP_BIN} start --discovery-poll-interval 3s --run-controllers > ${KCP_LOG_FILE} 2>&1 &
KCP_PID=$!

if ! ps -p ${KCP_PID}; then
  echo "####"
  echo "---> KCP failed to start, see ${KCP_LOG_FILE} for info."
  echo "####"
  exit 1 #this will trigger cleanup function
fi

echo "Waiting 15 seconds..."
sleep 15

#2. Create kcp-glbc workspace
createKCPWorkspace

#3. Create KCP Workload clusters
echo "Creating $NUM_CLUSTERS kcp workload cluster(s)."
port80=8081
port443=8444
for cluster in $CLUSTERS
do
  createKCPWorkloadCluster "$cluster" $port80 $port443
  port80=$((port80+1))
  port443=$((port443+1))
done

#4. Create GLBC cluster
echo "Creating glbc cluster."
createGLBCCluster $port80 $port443

echo ""
echo "KCP PID          : ${KCP_PID}"
echo ""
echo "The kind k8s clusters have been registered, and KCP is running, now you should run kcp-glbc."
echo ""
echo "Run locally:"
echo ""
echo "       ./bin/kcp-glbc --kubeconfig .kcp/admin.kubeconfig --context system:admin --glbc-kubeconfig ${TEMP_DIR}/${KCP_GLBC_KUBECONFIG}"
echo ""
echo "Run monitored in the $KCP_GLBC_CLUSTER_NAME cluster:"
echo ""
echo "       ./utils/local-setup-deploy-glbc.sh"
echo ""
echo "Don't forget to export the proper KUBECONFIG to create objects against KCP:"
echo "export KUBECONFIG=${PWD}/.kcp/admin.kubeconfig"
echo ""
read -p "Press enter to exit -> It will kill the KCP process running in background"
