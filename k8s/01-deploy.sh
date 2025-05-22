#!/bin/bash
set -euo pipefail

(
  cd .. && make image
)

kubectl create ns pgrwl-test --dry-run=client -oyaml | kubectl apply -f -

kubectl -n pgrwl-test create secret generic minio-certs \
  --from-file=public.crt=./files/minio/certs/public.crt \
  --from-file=private.key=./files/minio/certs/private.key \
  --dry-run=client -oyaml | kubectl apply -f -

kubectl apply -f manifests/
kubectl -n pgrwl-test rollout restart sts postgres
kubectl -n pgrwl-test rollout restart sts minio
kubectl -n pgrwl-test rollout restart sts pgrwl
