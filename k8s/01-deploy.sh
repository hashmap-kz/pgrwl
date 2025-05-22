#!/bin/bash
set -euo pipefail

(
  cd .. && make image
)

kubectl create ns pgrwl-test --dry-run -oyaml | kubectl apply -f -

kubectl -n pgrwl-test create secret generic minio-certs \
  --from-file=public.crt=./files/minio/certs/public.crt \
  --from-file=private.key=./files/minio/certs/private.key

kubectl apply -f manifests/
kubectl -n pgrwl-test rollout restart sts pgrwl
kubectl -n pgrwl-test rollout restart sts postgres
