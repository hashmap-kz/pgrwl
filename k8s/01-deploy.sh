#!/bin/bash
set -euo pipefail

(
  cd .. && make image
)

kubectl apply -f manifests/
kubectl -n pgrwl-test rollout restart sts pgrwl
kubectl -n pgrwl-test rollout restart sts postgres
