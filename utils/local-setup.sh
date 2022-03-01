#!/bin/bash
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
set -e pipefail

trap cleanup EXIT 1 2 3 6 15

cleanup() {
  echo "Killing KCP"
  kill "$CONTROLLER_2"
  kill "$KCP_PID"
  kill "$INGRESS_CONTROLLER_PID"
}

GOROOT=$(go env GOROOT)
export GOROOT
export KIND_BIN="./bin/kind"
export KCP_BIN="./bin/kcp"
export INGRESS_CONTROLLER_BIN="./bin/kcp-ingress-controller"
TEMP_DIR="./tmp"
KCP_LOG_FILE="${TEMP_DIR}"/kcp.log
INGRESS_CONTROLLER_LOG_FILE="${TEMP_DIR}"/ingress-controller.log

KIND_CLUSTER_PREFIX="kcp-cluster-"
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
${KCP_BIN} start --push-mode --run-controllers --resources-to-sync=deployments --resources-to-sync=services --resources-to-sync=ingresses.networking.k8s.io --auto-publish-apis > ${KCP_LOG_FILE} 2>&1 &
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

echo ""
echo "Starting Ingress Controller"
"${INGRESS_CONTROLLER_BIN}" --kubeconfig="${KUBECONFIG}" --envoy-listener-port=0 --envoy-xds-port=0 &> ${INGRESS_CONTROLLER_LOG_FILE} &
INGRESS_CONTROLLER_PID=$!

echo "Waiting 15 seconds..."
sleep 15

if ! ps -p ${INGRESS_CONTROLLER_PID}; then
  echo "####"
  echo "---> Ingress-controller failed to start, see ${INGRESS_CONTROLLER_LOG_FILE} for info."
  echo "####"
  exit 1 #this will trigger cleanup function
fi
echo "Registering kind k8s clusters into KCP"
kubectl apply -f ./tmp/

echo "Registering HCG APIs"
kubectl apply -f ./config/crd

./bin/deployment-splitter --kubeconfig=.kcp/admin.kubeconfig >> ${KCP_LOG_FILE} 2>&1 &
CONTROLLER_2=$!

echo ""
echo "The kind k8s clusters have been registered, and KCP is running, now you should run the kcp-ingress"
echo "example: "
echo ""
echo "       ./bin/ingress-controller -kubeconfig .kcp/admin.kubeconfig"
echo ""
echo "Dont forget to export the proper KUBECONFIG to create objects against KCP:"
echo "export KUBECONFIG=${PWD}/.kcp/admin.kubeconfig"
echo ""
read -p "Press enter to exit -> It will kill the KCP process running in background"
