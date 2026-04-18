#!/bin/bash
set -euo pipefail

LOCALDEV=true
IMAGE="quay.io/pgrwl/pgrwl:latest"

if [[ "$LOCALDEV" == "true" ]]; then
  (
    cd ../../../ && make image
  )
  IMAGE='localhost:5000/pgrwl:latest'
fi

sed -i "s|__PGRWL_IMAGE__|${IMAGE}|g" manifests/04-pgrwl-receive.yaml
sed -i "s|__PGRWL_IMAGE__|${IMAGE}|g" manifests/05-pgrwl-backup.yaml

kubectl create ns pgrwl-test --dry-run=client -oyaml | kubectl apply -f -
kubectl create ns mon --dry-run=client -oyaml | kubectl apply -f -

# create grafana dashboards
kubectl create configmap grafana-dashboards \
  --from-file=dashboards/ \
  --namespace=mon \
  --dry-run=client -oyaml | kubectl apply -f -

# prepare various configs (loki, promtail, etc...)
while IFS= read -r -d '' filename; do
  name=$(basename "${filename}")
  kubectl -n mon \
    create configmap "${name}" \
    --from-file="${filename}" --dry-run=client -o yaml | kubectl apply -f -
done < <(find "manifests/configs" -type f -print0)

# create minio certs
kubectl -n pgrwl-test create secret generic minio-certs \
  --from-file=public.crt=./files/minio/certs/public.crt \
  --from-file=private.key=./files/minio/certs/private.key \
  --dry-run=client -oyaml | kubectl apply -f -

# deploy and restart workloads
kubectl apply -f manifests/
kubectl -n pgrwl-test rollout restart sts postgres
kubectl -n pgrwl-test rollout restart sts minio
kubectl -n pgrwl-test rollout restart sts pgrwl-receive
kubectl -n pgrwl-test rollout restart sts pgrwl-backup
kubectl -n mon rollout restart sts prometheus
kubectl -n mon rollout restart deploy grafana
kubectl -n mon rollout restart sts loki
kubectl -n mon rollout restart ds promtail
