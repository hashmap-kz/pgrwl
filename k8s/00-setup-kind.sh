#!/bin/bash
set -euo pipefail

# prepare config for the 'kind' cluster
cat <<EOF >kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: "pgrwl"
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30265
        hostPort: 30265
        protocol: TCP
      - containerPort: 30266
        hostPort: 30266
        protocol: TCP
EOF

# setup cluster with kind, to safely test in a sandbox
if kind get clusters | grep "pgrwl"; then
  kind delete clusters "pgrwl"
fi
kind create cluster --config=kind-config.yaml
kubectl config set-context "kind-pgrwl"
rm -f kind-config.yaml
