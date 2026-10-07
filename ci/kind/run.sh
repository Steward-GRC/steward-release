#!/usr/bin/env bash
# The chart's install test: a kind cluster with the bring-your-own
# dependencies in deps.yaml, the umbrella installed from this checkout with
# the service images built from ci/kind/images.txt, every Deployment Ready,
# the gateway's /readyz reporting every dependency up, and the ai and
# pdf-renderer operators each reconciling a resource of their own.
#
#   ci/kind/run.sh
#
# Needs kind, kubectl, helm and docker on PATH, the images already built
# (ci/kind/build-image.sh) or saved in $IMAGE_DIR/<alias>.tar, and an umbrella
# chart directory with its dependencies built in $CHART_DIR (default: the
# repo root). KIND_CLUSTER names the cluster (default steward-ci); it is
# created unless it already exists, and never deleted here. A reused cluster
# keeps the images already on its node and the generated Secrets (so its
# Postgres keeps working). KUBECONFIG is set to the cluster's own file, never
# your default context.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"
cluster="${KIND_CLUSTER:-steward-ci}"
ns=steward
chart="${CHART_DIR:-$root}"
node_image="${KIND_NODE_IMAGE:-kindest/node:v1.34.11@sha256:44e222ee2132dab25ff87301682f89eb82c7880ea3a1bf543bfe9708fd08d67d}"
work="${WORK_DIR:-$(mktemp -d)}"
export KUBECONFIG="$work/kubeconfig"

log() { echo "[$(date -u +%H:%M:%S)] $*"; }

diagnose() {
  log "FAILED: cluster state"
  kubectl -n "$ns" get pods -o wide || true
  kubectl -n "$ns" get events --sort-by=.lastTimestamp | tail -60 || true
  for p in $(kubectl -n "$ns" get pods -o name 2>/dev/null); do
    echo "----- $p"
    kubectl -n "$ns" logs "$p" --all-containers --tail=40 2>&1 || true
    kubectl -n "$ns" logs "$p" --all-containers --previous --tail=40 2>/dev/null || true
  done
}
trap 'diagnose' ERR

aliases=$(awk '!/^#/ && NF { print $1 }' "$here/images.txt")

if kind get clusters 2>/dev/null | grep -qx "$cluster"; then
  log "using the existing kind cluster $cluster"
  kind export kubeconfig --name "$cluster" --kubeconfig "$KUBECONFIG"
else
  log "creating kind cluster $cluster ($node_image)"
  kind create cluster --name "$cluster" --image "$node_image" --kubeconfig "$KUBECONFIG" --wait 120s
fi

for a in $aliases; do
  commit=$(awk -v a="$a" '$1 == a { print $3 }' "$here/images.txt")
  tag="steward-ci/$a:${commit:0:12}"
  if docker exec "$cluster-control-plane" crictl inspecti -q "docker.io/$tag" >/dev/null 2>&1; then
    log "$tag already on the node"
    continue
  fi
  if [ -n "${IMAGE_DIR:-}" ]; then
    kind load image-archive --name "$cluster" "$IMAGE_DIR/$a.tar" >/dev/null
  else
    kind load docker-image --name "$cluster" "$tag" >/dev/null
  fi
  log "loaded $tag"
done

kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# Dev-only secrets, generated on the first run in a cluster and never printed.
# A reused cluster keeps them: Postgres only reads its init script and
# passwords when its data directory is empty, so new ones would lock every
# service out.
if kubectl -n "$ns" get secret steward-ci-postgres-init steward-ci-deps steward-ci-env >/dev/null 2>&1; then
  log "reusing the secrets already in namespace $ns"
else
  rand() { openssl rand -hex "${1:-24}"; }
  pg_init="$work/init.sql"
  : > "$pg_init"
  pw_args=()
  for s in identity core workflow obligations audit delivery collab ai reporting; do
    pw=$(rand)
    echo "CREATE ROLE $s LOGIN PASSWORD '$pw'; CREATE DATABASE steward_$s OWNER $s;" >> "$pg_init"
    pw_args+=(--from-literal="$s-password=$pw")
  done
  echo '\connect steward_ai' >> "$pg_init"
  echo 'CREATE EXTENSION IF NOT EXISTS vector;' >> "$pg_init"
  rabbit_pw=$(rand)
  kubectl -n "$ns" create secret generic steward-ci-postgres-init --from-file=init.sql="$pg_init" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$ns" create secret generic steward-ci-deps \
    --from-literal=postgres-password="$(rand)" \
    --from-literal=rabbitmq-password="$rabbit_pw" \
    --from-literal=kratos-cookie-secret="[\"$(rand 32)\"]" \
    --from-literal=kratos-cipher-secret="[\"$(rand 16)\"]" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$ns" create secret generic steward-ci-env "${pw_args[@]}" \
    --from-literal=rabbitmq-url="amqp://steward:$rabbit_pw@steward-rabbitmq:5672/" \
    --from-literal=core-settings-key="$(openssl rand -base64 32)" \
    --from-literal=ai-settings-key="$(openssl rand -base64 32)" \
    --from-literal=totp-enc-key="$(rand 32)" \
    --from-literal=collab-token-secret="$(rand 32)" \
    --from-literal=setup-token="$(rand)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  rm -f "$pg_init"
  log "secrets created"
fi

# Object storage for PDF export: the one Secret delivery and the render Jobs
# read, generated on the first run like the others and never printed.
if ! kubectl -n "$ns" get secret steward-pdf-renderer-s3 >/dev/null 2>&1; then
  kubectl -n "$ns" create secret generic steward-pdf-renderer-s3 \
    --from-literal=AWS_S3_ENDPOINT=http://steward-s3:9000 \
    --from-literal=S3_BUCKET=steward-pdf \
    --from-literal=AWS_REGION=us-east-1 \
    --from-literal=AWS_S3_FORCE_PATH_STYLE=true \
    --from-literal=AWS_ACCESS_KEY_ID="$(openssl rand -hex 10)" \
    --from-literal=AWS_SECRET_ACCESS_KEY="$(openssl rand -hex 20)" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  log "object-storage secret created"
fi

kubectl -n "$ns" apply -f "$here/deps.yaml" >/dev/null
kubectl -n "$ns" rollout status deploy --timeout=300s
log "dependencies ready"

# The PDF bucket, created through the S3 API with the Secret's own keys
# (an existing bucket answers 409, which is fine).
kubectl -n "$ns" delete job steward-ci-s3-bucket --ignore-not-found >/dev/null
kubectl -n "$ns" apply -f - >/dev/null <<'EOF'
apiVersion: batch/v1
kind: Job
metadata:
  name: steward-ci-s3-bucket
spec:
  backoffLimit: 5
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: create-bucket
          image: docker.io/curlimages/curl:8.22.0@sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777
          envFrom:
            - secretRef: {name: steward-pdf-renderer-s3}
          command:
            - sh
            - -c
            - |
              code=$(curl -sS -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:${AWS_REGION}:s3" \
                --user "${AWS_ACCESS_KEY_ID}:${AWS_SECRET_ACCESS_KEY}" -X PUT "${AWS_S3_ENDPOINT}/${S3_BUCKET}")
              echo "create bucket ${S3_BUCKET}: HTTP ${code}"
              [ "$code" = 200 ] || [ "$code" = 409 ]
EOF
kubectl -n "$ns" wait job steward-ci-s3-bucket --for=condition=Complete --timeout=120s >/dev/null
done_pod=$(kubectl -n "$ns" get pods -l job-name=steward-ci-s3-bucket --field-selector=status.phase=Succeeded -o name | head -1)
kubectl -n "$ns" logs "$done_pod" --tail=1

image_args=()
for a in $aliases; do
  commit=$(awk -v a="$a" '$1 == a { print $3 }' "$here/images.txt")
  image_args+=(--set "$a.image.repository=steward-ci/$a" --set "$a.image.tag=${commit:0:12}")
done

log "helm install"
helm upgrade --install steward "$chart" --namespace "$ns" \
  -f "$root/values.yaml" -f "$root/values-byo-postgres-example.yaml" -f "$here/values-kind.yaml" \
  "${image_args[@]}" --wait=false

log "waiting for every Deployment"
kubectl -n "$ns" wait deploy -l app.kubernetes.io/part-of=steward --for=condition=Available --timeout=600s
kubectl -n "$ns" rollout status deploy -l app.kubernetes.io/part-of=steward --timeout=60s
kubectl -n "$ns" get deploy

log "gateway /readyz"
# `kubectl run --rm -i` can lose a short-lived pod's output, so an empty
# answer is retried; a real HTTP code is not.
code=""
for attempt in 1 2 3; do
  ready=$(kubectl -n "$ns" run "readyz-check-$attempt" --rm -i --restart=Never --quiet \
    --image=docker.io/curlimages/curl:8.22.0@sha256:58adaa4e8dca9c988bae2aba4ab3434a0bb2da16bbe3f92dec39ec7785166777 \
    -- curl -sS -w '\n%{http_code}' http://steward-gateway:8080/readyz || true)
  echo "$ready"
  code=$(echo "$ready" | tail -1)
  [ -n "$code" ] && break
  log "no answer from the readyz check (attempt $attempt)"
done
if [ "$code" != 200 ]; then
  log "gateway /readyz answered $code"
  diagnose
  exit 1
fi

# Workload auth: an enabled callee must be able to fetch the JWKS with the
# credentials the chart mounts.
if kubectl -n "$ns" logs -l app.kubernetes.io/part-of=steward --all-containers --tail=-1 --prefix 2>/dev/null \
    | grep -E 'jwks.*status 401|WORKLOAD_OIDC_CA_FILE.*no such file'; then
  log "a callee could not fetch the JWKS"
  exit 1
fi

# The operators: each must take a resource of its own through to a status,
# with only the namespaced RBAC the chart gives it.
await_phase() { # <resource> <name> <phase regex> <seconds>
  local phase=""
  for _ in $(seq "$4"); do
    phase=$(kubectl -n "$ns" get "$1" "$2" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if echo "$phase" | grep -qE "^($3)$"; then
      log "$1/$2 reached phase $phase"
      return 0
    fi
    sleep 1
  done
  log "$1/$2 is still in phase '${phase}' after $4s"
  return 1
}

log "ai operator: reconcile a PolicyAIJob"
aijob="ci-reconcile-$(date +%s)"
kubectl -n "$ns" apply -f - >/dev/null <<EOF
apiVersion: ai.steward-grc.com/v1alpha1
kind: PolicyAIJob
metadata:
  name: $aijob
spec:
  operation: RELATIONSHIP_LEARN
EOF
await_phase policyaijobs.ai.steward-grc.com "$aijob" 'Succeeded|Failed' 120
kubectl -n "$ns" get policyaijobs.ai.steward-grc.com "$aijob"
kubectl -n "$ns" delete policyaijobs.ai.steward-grc.com "$aijob" --ignore-not-found >/dev/null

log "pdf-renderer operator: reconcile a PdfRender into a render Job"
render="ci-reconcile-$(date +%s)"
pv=00000000-0000-4000-8000-000000000000
kubectl -n "$ns" apply -f - >/dev/null <<EOF
apiVersion: renders.steward-grc.com/v1alpha1
kind: PdfRender
metadata:
  name: $render
spec:
  policyVersionId: $pv
  fetchURL: http://steward-delivery:8082/internal/policies/$pv/html
  outputBucket: steward-ci
  outputKey: artifacts/$pv/$render.pdf
  sensitivity: standard
EOF
await_phase pdfrenders.renders.steward-grc.com "$render" 'Pending|Running|Succeeded|Failed' 60
kubectl -n "$ns" wait job "$render" --for=create --timeout=60s >/dev/null
sa=$(kubectl -n "$ns" get job "$render" -o jsonpath='{.spec.template.spec.serviceAccountName}')
if [ "$sa" != steward-pdf-renderer ]; then
  log "the render Job runs as '$sa', not steward-pdf-renderer"
  exit 1
fi
log "render Job $render created, running as $sa"
kubectl -n "$ns" delete pdfrenders.renders.steward-grc.com "$render" --ignore-not-found --wait=false >/dev/null

# Read the log first: grep -q closing the pipe early fails it under pipefail.
delivery_log=$(kubectl -n "$ns" logs deploy/steward-delivery --tail=-1 2>/dev/null || true)
if ! grep -q '"PDF export on"' <<<"$delivery_log"; then
  log "delivery did not turn PDF export on"
  exit 1
fi
log "delivery: PDF export on"

if kubectl -n "$ns" logs -l 'app.kubernetes.io/name in (steward-ai-operator,steward-pdf-renderer,steward-delivery)' \
    --all-containers --tail=-1 --prefix 2>/dev/null | grep -E 'is forbidden'; then
  log "an operator was refused an API call"
  exit 1
fi

trap - ERR
log "PASS: every Deployment Ready, gateway /readyz 200, both operators reconciling"
