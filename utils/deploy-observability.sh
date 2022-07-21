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
source "${DEPLOY_SCRIPT_DIR}"/.setupEnv
source "${DEPLOY_SCRIPT_DIR}"/.startUtils

set -e pipefail

#Hash of root:users:ev:vo:system-apiserver:kcp-glbc, this will work for all local deployments using the kcp-glbc workspace
GLBC_NAMESPACE=kcp89b5fd4ba9405ee7b18d0da859ce7420d36926bac4a97e01af5c244a
PROMETHEUS_NAMESPACE=monitoring

kubectl config use-context kind-kcp-cluster-glbc-control

# Deploy monitoring stack (includes prometheus, alertmanager & grafana)
wait_for "./bin/kustomize build config/observability/kubernetes | kubectl apply --force-conflicts  --server-side -f -" "prometheus" "2m" "10"

# Wait for all monitoring to be available
kubectl -n ${PROMETHEUS_NAMESPACE} wait --timeout=300s --for=condition=Available deployments --all

# Deploy Pod Monitor for kcp-glbc
kubectl -n ${GLBC_NAMESPACE} apply -f config/observability/kubernetes/monitoring_resources/podmonitor-kcp-glbc-controller-manager.yaml

# Check kcp-glbc Prometheus config
wait_for "kubectl -n ${PROMETHEUS_NAMESPACE} get secret prometheus-k8s -o json | jq -r '.data[\"prometheus.yaml.gz\"]'| base64 -d | gunzip | grep kcp-glbc" "kcp-glbc prometheus config" "1m" "10"

ports=$(docker ps --format '{{json .}}' | jq 'select(.Names == "kcp-cluster-glbc-control-control-plane").Ports')
httpport=$(echo $ports | sed -e 's/.*0.0.0.0\:\(.*\)->80\/tcp.*/\1/')

# Check Prometheus Target
wait_for "curl -s http://prometheus.127.0.0.1.nip.io:$httpport/api/v1/targets | jq -e '.data.activeTargets[] | select(.labels.job == \"${GLBC_NAMESPACE}/kcp-glbc-controller-manager\").labels'" "kcp-glbc prometheus target" "1m" "10"

echo ""
echo "Monitoring consoles:"
echo ""
echo "     Prometheus: http://prometheus.127.0.0.1.nip.io:$httpport"
echo "     Grafana:    http://grafana.127.0.0.1.nip.io:$httpport     (user:admin, password: admin)"
echo ""
