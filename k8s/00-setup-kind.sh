#!/bin/bash
set -euo pipefail

docker run -d --restart=always -p 5000:5000 --name kind-registry registry:2

# prepare config for the 'kind' cluster
cat <<EOF >kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:5000"]
      endpoint = ["http://kind-registry:5000"]
name: "pgrwl"
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30000
        hostPort: 30000
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

docker network connect kind kind-registry

