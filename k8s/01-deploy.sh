#!/bin/bash
set -euo pipefail

(
  cd .. && make image
)

kubectl create ns pgrwl-test --dry-run=client -oyaml | kubectl apply -f -

# create grafana dashboards
kubectl create configmap grafana-dashboards \
  --from-file=dashboards/ \
  --namespace=pgrwl-test \
  --dry-run=client -oyaml | kubectl apply -f -

# create minio certs
kubectl -n pgrwl-test create secret generic minio-certs \
  --from-file=public.crt=./files/minio/certs/public.crt \
  --from-file=private.key=./files/minio/certs/private.key \
  --dry-run=client -oyaml | kubectl apply -f -

# deploy and restart workloads
kubectl apply -f manifests/
kubectl -n pgrwl-test rollout restart sts postgres
kubectl -n pgrwl-test rollout restart sts minio
kubectl -n pgrwl-test rollout restart sts pgrwl
kubectl -n pgrwl-test rollout restart sts prometheus
kubectl -n pgrwl-test rollout restart deploy grafana
