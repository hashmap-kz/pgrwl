#!/bin/bash
set -euo pipefail

echo "✓ Stopping and removing registry container..."
docker rm -f kind-registry 2>/dev/null || echo "Registry container not running"

echo "✓ Disconnecting registry from 'kind' network..."
docker network disconnect kind kind-registry 2>/dev/null || echo "Already disconnected"

echo "✓ Deleting kind cluster..."
kind delete cluster --name pgrwl || echo "Kind cluster not found"

echo "✓ Running local registry..."
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
      # postgres
      - containerPort: 30265
        hostPort: 30265
        protocol: TCP
      # pgrwl
      - containerPort: 30266
        hostPort: 30266
        protocol: TCP
      # minio-ui
      - containerPort: 30267
        hostPort: 30267
        protocol: TCP
      # minio-server
      - containerPort: 30268
        hostPort: 30268
        protocol: TCP
EOF

# setup cluster with kind, to safely test in a sandbox
kind create cluster --config=kind-config.yaml
kubectl config set-context "kind-pgrwl"
docker network connect kind kind-registry

# cleanup
rm -f kind-config.yaml
