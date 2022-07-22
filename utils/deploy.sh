#!/bin/bash

#
# Copyright 2022 Red Hat, Inc.
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

DEPLOY_SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
KCP_GLBC_DIR="${DEPLOY_SCRIPT_DIR}/.."
source "${DEPLOY_SCRIPT_DIR}"/.setupEnv
source "${DEPLOY_SCRIPT_DIR}"/.startUtils

#Workspace
ORG_WORKSPACE=root:default
GLBC_WORKSPACE=kcp-glbc
GLBC_WORKSPACE_COMPUTE=${GLBC_WORKSPACE}-compute
GLBC_WORKSPACE_USER=${GLBC_WORKSPACE}-user
GLBC_WORKSPACE_USER_COMPUTE=${GLBC_WORKSPACE_USER}-compute

#Workload clusters
: ${GLBC_WORKLOAD_CLUSTER_NAME:=glbc}
: ${GLBC_USER_WORKLOAD_CLUSTER_NAME:=glbc-user}

: ${KCP_VERSION:="release-0.6"}
KCP_SYNCER_IMAGE="ghcr.io/kcp-dev/kcp/syncer:${KCP_VERSION}"

# GLBC Deployment
GLBC_NAMESPACE=kcp-glbc
DEPLOY_COMPONENTS=glbc,cert-manager
GLBC_KUSTOMIZATION=${KCP_GLBC_DIR}/config/deploy/local
MULTI_WORKSPACE_AWARE=true

# Misc
# Wait for workload clusters to be ready before continuing
: ${WAIT_WC_READY:="false"}

############################################################
# Help                                                     #
############################################################
help()
{
   # Display Help
   echo "Prepares a KCP workspace and deploys GLBC and its dependant components into it."
   echo
   echo "Syntax: deploy.sh [-c|k|m|n|h|w|W]"
   echo "options:"
   echo "c     Components to deploy (default: ${DEPLOY_COMPONENTS})"
   echo "k     GLBC deployment kustomization directory (default: ${GLBC_KUSTOMIZATION})"
   echo "m     Multi Workspace aware deployment (default: ${MULTI_WORKSPACE_AWARE})"
   echo "n     Namespace glbc is being deployed into (default: ${GLBC_NAMESPACE})"
   echo "h     Print this Help."
   echo "w     Workspace to create and use for deployment (default: ${GLBC_WORKSPACE})."
   echo "W     Organisation workspace (default: ${ORG_WORKSPACE})."
   echo
}

############################################################
# print_env                                                #
############################################################
print_env()
{
   echo "Current deployment configuration"
   echo
   echo "KubeConfig:"
   echo
   echo "  KUBECONFIG:                    ${KUBECONFIG}"
   echo
   echo "Workspaces:"
   echo
   echo "  ORG_WORKSPACE:                    ${ORG_WORKSPACE}"
   echo "  GLBC_WORKSPACE:                   ${GLBC_WORKSPACE}"
   echo "  GLBC_WORKSPACE_USER:              ${GLBC_WORKSPACE_USER}"
   echo "  GLBC_WORKSPACE_COMPUTE:           ${GLBC_WORKSPACE_COMPUTE}"
   echo "  GLBC_WORKSPACE_USER_COMPUTE:      ${GLBC_WORKSPACE_USER_COMPUTE}"
   echo
   echo "Workload clusters:"
   echo
   echo "  GLBC_WORKLOAD_CLUSTER_NAME:       ${GLBC_WORKLOAD_CLUSTER_NAME}"
   echo "  GLBC_USER_WORKLOAD_CLUSTER_NAME:  ${GLBC_USER_WORKLOAD_CLUSTER_NAME}"
   echo "  KCP_SYNCER_IMAGE:                 ${KCP_SYNCER_IMAGE}"
   echo
   echo "GLBC Deployment:"
   echo
   echo "  GLBC_NAMESPACE:                   ${GLBC_NAMESPACE}"
   echo "  DEPLOY_COMPONENTS:                ${DEPLOY_COMPONENTS}"
   echo "  GLBC_KUSTOMIZATION:               ${GLBC_KUSTOMIZATION}"
   echo "  MULTI_WORKSPACE_AWARE:            ${MULTI_WORKSPACE_AWARE}"
   echo
   echo "Misc:"
   echo
   echo "  WAIT_WC_READY                     ${WAIT_WC_READY}"
   echo
}

create_api_binding() {
  name=$1;
  exportName=$2;
  path=$3;
  cat <<EOF | kubectl apply -f -
apiVersion: apis.kcp.dev/v1alpha1
kind: APIBinding
metadata:
  name: ${name}
spec:
  reference:
    workspace:
      path: ${path}
      exportName: ${exportName}
EOF
  kubectl wait --timeout=60s --for=condition=Ready=true apibinding $name
}

create_ns() {
  echo "Creating namespace '${1}'"
  kubectl create namespace ${1} --dry-run=client -o yaml | kubectl apply -f -
}

create_workload_cluster() {
  kubectl get synctargets ${GLBC_WORKLOAD_CLUSTER_NAME} || {
    echo "Creating workload cluster '${1}'"
    ${KUBECTL_KCP_BIN} workload sync ${1} --kcp-namespace kcp-syncer --syncer-image=${KCP_SYNCER_IMAGE} --resources=ingresses.networking.k8s.io,services --output-file ${GLBC_KUSTOMIZATION}/${1}-syncer.yaml
    echo "Apply the following syncer config to the intended physical cluster."
    echo ""
    echo "   kubectl apply -f ${GLBC_KUSTOMIZATION}/${1}-syncer.yaml"
    echo ""
  }
  kubectl wait --timeout=60s --for=condition=VirtualWorkspaceURLsReady=true apiexport kubernetes

  if [[ $WAIT_WC_READY = "true" ]]; then
    echo "This script will automatically continue once the cluster is synced!"
    echo "Waiting for workload cluster ${1} to be ready ..."
    kubectl wait --timeout=300s --for=condition=Ready=true synctargets ${1}
  fi
}

deploy_cert_manager() {
  echo "Deploying Cert Manager"
  create_ns "cert-manager"
  kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/cert-manager.yaml
  echo "Waiting for Cert Manager deployments to be ready..."
  kubectl -n cert-manager wait --timeout=300s --for=condition=Available deployments --all
}

deploy_glbc() {
  echo "Creating GLBC namespace"
  create_ns ${GLBC_NAMESPACE}

  echo "Creating issuer"
  go run ${DEPLOY_SCRIPT_DIR}/certman-issuer/ --glbc-kubeconfig ${GLBC_KUSTOMIZATION}/kcp.kubeconfig --issuer-namespace ${GLBC_NAMESPACE}

  echo "Deploying GLBC"
  ${KUSTOMIZE_BIN} build ${GLBC_KUSTOMIZATION} | kubectl apply -f -
  echo "Waiting for GLBC deployments to be ready..."
  kubectl -n ${GLBC_NAMESPACE} wait --timeout=300s --for=condition=Available deployments --all
}

deploy_glbc_observability() {
    echo "Deploying GLBC Observability"
    create_ns "kcp-glbc-observability"
    ## Deploy Grafana
    wait_for "${KUSTOMIZE_BIN} build config/observability/kubernetes/grafana/ | kubectl apply -f -" "grafana" "1m" "5"
    echo "Waiting for Observability deployments to be ready..."
    kubectl -n kcp-glbc-observability wait --timeout=300s --for=condition=Available deployments --all
    ## Deploy Pod Monitor for kcp-glbc
    ${KUSTOMIZE_BIN} build config/prometheus/ | kubectl -n ${GLBC_NAMESPACE} apply -f -
}

############################################################
# Script Start                                             #
############################################################

while getopts "c:k:m:n:hw:W:" arg; do
  case "${arg}" in
    c)
      DEPLOY_COMPONENTS=${OPTARG}
      ;;
    k)
      GLBC_KUSTOMIZATION=${OPTARG}
      ;;
    m)
      MULTI_WORKSPACE_AWARE=${OPTARG}
      ;;
    n)
      GLBC_NAMESPACE=${OPTARG}
      ;;
    h)
      help
      exit 0
      ;;
    w)
      GLBC_WORKSPACE=${OPTARG}
      GLBC_WORKSPACE_COMPUTE=${GLBC_WORKSPACE}-compute
      GLBC_WORKSPACE_USER=${GLBC_WORKSPACE}-user
      GLBC_WORKSPACE_USER_COMPUTE=${GLBC_WORKSPACE_USER}-compute
      ;;
    W)
      ORG_WORKSPACE=${OPTARG}
      ;;
    *)
      help
      exit 1
      ;;
  esac
done
shift $((OPTIND-1))

set -e pipefail

## Check we are targeting a kcp instance
${KUBECTL_KCP_BIN} workspace > /dev/null || (echo "You must be targeting a KCP API Server, check your current KUBECONIFG and context before continuing!" && exit 1)

print_env
echo "Continuing in 10 seconds, Ctrl+C to stop ..."
sleep 10

## Get the ca data for this KCP if it exists, used later to inject into generated kubeconfigs
caData=$(kubectl config view --raw -o json | jq -r '.clusters[0].cluster."certificate-authority-data"' | tr -d '"')

############################################################
# GLBC Compute Service Workspace (kcp-glbc-compute)        #
############################################################

## Create glbc compute service workspace if it doesn't already exist
${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}
${KUBECTL_KCP_BIN} workspace create ${GLBC_WORKSPACE_COMPUTE} --enter || ${KUBECTL_KCP_BIN} workspace use ${GLBC_WORKSPACE_COMPUTE}

## Create GLBC workload cluster
kubectl create namespace kcp-syncer --dry-run=client -o yaml | kubectl apply -f -
create_workload_cluster ${GLBC_WORKLOAD_CLUSTER_NAME}

## Add location
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/location.yaml

############################################################
# GLBC Workspace (kcp-glbc)                                #
############################################################

## Create glbc workspace if it doesn't already exist
${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}
${KUBECTL_KCP_BIN} workspace create ${GLBC_WORKSPACE} --enter || ${KUBECTL_KCP_BIN} workspace use ${GLBC_WORKSPACE}

## Bind to compute APIs
create_api_binding "kubernetes" "kubernetes" "${ORG_WORKSPACE}:${GLBC_WORKSPACE_COMPUTE}"

## Register the Pod API (required by cert-manager)
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/crds/pods.yaml

## Register GLBC APIs
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/apiresourceschema.yaml
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/apiexport.yaml

kubectl wait --timeout=60s --for=condition=VirtualWorkspaceURLsReady=true apiexport glbc

create_api_binding "glbc" "glbc" "${ORG_WORKSPACE}:${GLBC_WORKSPACE}"

## Register CertManager APIs
kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/certificates-apiresourceschema.yaml
kubectl apply -f ${KCP_GLBC_DIR}/config/cert-manager/cert-manager-apiexport.yaml
create_api_binding "cert-manager" "cert-manager-stable" "${ORG_WORKSPACE}:${GLBC_WORKSPACE}"

## Create cluster scoped SA for glbc to use (Currently watches a single workspace)
${DEPLOY_SCRIPT_DIR}/create_glbc_ns.sh -a ${caData} -n "default" -c ${GLBC_WORKSPACE} -C

###############################################################
# GLBC User Compute Service Workspace (kcp-glbc-user-compute) #
###############################################################

## Create glbc user compute service workspace if it doesn't already exist
${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}
${KUBECTL_KCP_BIN} workspace create ${GLBC_WORKSPACE_USER_COMPUTE} --enter || ${KUBECTL_KCP_BIN} workspace use ${GLBC_WORKSPACE_USER_COMPUTE}

## Create User workload cluster
kubectl create namespace kcp-syncer --dry-run=client -o yaml | kubectl apply -f -
create_workload_cluster ${GLBC_USER_WORKLOAD_CLUSTER_NAME}

## Add location
kubectl apply -f ${KCP_GLBC_DIR}/utils/kcp-contrib/location.yaml

############################################################
# GLBC User Workspace (kcp-glbc-user)                      #
############################################################

## Create glbc user workspace if it doesn't already exist
${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}
${KUBECTL_KCP_BIN} workspace create ${GLBC_WORKSPACE_USER} --enter || ${KUBECTL_KCP_BIN} workspace use ${GLBC_WORKSPACE_USER}
## Bind to compute APIs
create_api_binding "kubernetes" "kubernetes" "${ORG_WORKSPACE}:${GLBC_WORKSPACE_USER_COMPUTE}"
## Bind to GLBC APIs
create_api_binding "glbc" "glbc" "${ORG_WORKSPACE}:${GLBC_WORKSPACE}"

createGLBCNSOptions=""
if [[ ! $MULTI_WORKSPACE_AWARE = true ]]; then
  # If it's not multi workspace aware we replace the deployments kcp kubeconfig with the generated one giving access to the single workspace
  createGLBCNSOptions="-o ${GLBC_KUSTOMIZATION}/kcp.kubeconfig"
fi
## Create cluster scoped SA for glbc to use (Currently watches a single workspace)
${DEPLOY_SCRIPT_DIR}/create_glbc_ns.sh -a ${caData} -n "default" -c ${GLBC_WORKSPACE_USER} -C ${createGLBCNSOptions}

############################################################
# Deploy GLBC Components                                   #
############################################################

${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}:${GLBC_WORKSPACE}

## Deploy components
if [[ $DEPLOY_COMPONENTS =~ "cert-manager" ]]; then
  deploy_cert_manager
fi

if [[ $DEPLOY_COMPONENTS =~ "glbc" ]]; then
  deploy_glbc
  if [[ $DEPLOY_COMPONENTS =~ "observability" ]]; then
    deploy_glbc_observability
  fi

  echo ""
  echo "GLBC is now running."
  echo ""
  echo "Try deploying the sample service:"
  echo ""
  echo "     ${KUBECTL_KCP_BIN} workspace use ${ORG_WORKSPACE}:${GLBC_WORKSPACE_USER}"
  echo "     kubectl apply -f samples/echo-service/echo.yaml"
  echo ""
fi
