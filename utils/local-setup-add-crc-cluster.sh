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

# Adds a CRC cluster to your local setup. You must run local-setup before running this script.
#
# Requires crc
# wget https://developers.redhat.com/content-gateway/file/pub/openshift-v4/clients/crc/1.40.0/crc-linux-amd64.tar.xz
# $ crc version
# CodeReady Containers version: 1.40.0+5966df09
# OpenShift version: 4.9.18 (embedded in executable)
#
# Get pull secret from here https://cloud.redhat.com/openshift/create/local, and save it to ~/pull-secret

# Note: Kubernetes versions of all clusters added to kcp need to be the same minor version for resources to sync correctly.
#
# $ kubectl --context crc-admin version -o json | jq .serverVersion.gitVersion
# "v1.22.3+e790d7f"
# $ kubectl --context kind-kcp-cluster-1 version -o json | jq .serverVersion.gitVersion
# "v1.22.7"
# $ kubectl --context kind-kcp-cluster-2 version -o json | jq .serverVersion.gitVersion
# "v1.22.7"
# $ kubectl get workloadclusters -o wide
# NAME              LOCATION          READY   SYNCED API RESOURCES
# glbc              glbc              True
# kcp-cluster-crc   kcp-cluster-crc   True
# kubectl get locations
# NAME         RESOURCE           AVAILABLE   INSTANCES   LABELS
# location-1   workloadclusters   2           2

set -e pipefail

TEMP_DIR="./tmp"
CRC_CLUSTER_NAME=kcp-cluster-crc
CRC_KUBECONFIG="${CRC_CLUSTER_NAME}.kubeconfig"
PULL_SECRET=~/pull-secret

KUBECTL_KCP_BIN="./bin/kubectl-kcp"

: ${KCP_VERSION:="release-0.6"}
KCP_SYNCER_IMAGE="ghcr.io/kcp-dev/kcp/syncer:${KCP_VERSION}"

crc config set enable-cluster-monitoring true

crc start -p $PULL_SECRET

cp ~/.crc/machines/crc/kubeconfig ${TEMP_DIR}/${CRC_KUBECONFIG}
cp ${TEMP_DIR}/${CRC_KUBECONFIG} ${TEMP_DIR}/${CRC_KUBECONFIG}.internal

echo "Registering crc cluster into KCP"
KUBECONFIG=config/deploy/local/kcp.kubeconfig ./bin/kubectl-kcp workspace use root:users:ev:vo:system-apiserver:kcp-glbc-compute
KUBECONFIG=config/deploy/local/kcp.kubeconfig ${KUBECTL_KCP_BIN} workload sync ${CRC_CLUSTER_NAME} --syncer-image=${KCP_SYNCER_IMAGE} --resources=ingresses.networking.k8s.io,services > ${TEMP_DIR}/${CRC_CLUSTER_NAME}-syncer.yaml
kubectl --context crc-admin apply -f ${TEMP_DIR}/${CRC_CLUSTER_NAME}-syncer.yaml

# TODO: Figure out the right order of cmds, kubeconfig, context & env vars to deploy the observability operator
# Notes from installing manually on a managed openshift cluster:
#
# Create and set values for the observability secret env file.
# The default respoitory location is https://github.com/Kuadrant/kcp-glbc/tree/main/config/observability/openshift/monitoring_resources.
# An index.json config file is read in by the observability operator from there.
# ```
# cp config/observability/openshift/observability-operator.env.template config/observability/openshift/observability-operator.env
# vi config/observability/openshift/observability-operator.env
# ```
# Create the various operator resources
# ```
# ./bin/kustomize build config/observability/openshift/ | oc apply -f -
# # This will fail to create the Observability resource the first time, but will create other operator resources
# ```
# Wait for the operator to be running, which usually means the CRD is registered
# ```
# oc wait --for=condition=available --timeout=120s -n observability-operator deployment/observability-operator-controller-manager
# ./bin/kustomize build config/observability/openshift/ | oc apply -f -
# ```
# Monitoring resources (GrafanaDashboards, PodMonitors etc..) are periodically checked for updates in git.
# The resyncPeriod in the Observability resource defaults to 1h.
#
echo "Install observability operator"
echo "TODO"
