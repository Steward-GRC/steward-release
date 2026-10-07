#!/usr/bin/env bash
# Fail unless the cluster-scoped objects in a rendered manifest are exactly
# the expected ones. The chart installs none by default; each one is an
# explicit opt-in.
#
#   helm template ... --include-crds | scripts/check-cluster-scoped.sh [Kind/name ...]
#
# With no arguments, any cluster-scoped object fails the check.
set -euo pipefail

cluster_scoped='CustomResourceDefinition|ClusterRole|ClusterRoleBinding|ValidatingWebhookConfiguration|MutatingWebhookConfiguration|ValidatingAdmissionPolicy|ValidatingAdmissionPolicyBinding|APIService|ClusterIssuer|IngressClass|StorageClass|PriorityClass|RuntimeClass|Namespace|PersistentVolume|CSIDriver|GatewayClass'

found=$(awk '
  /^---/ { kind = ""; inmeta = 0; next }
  /^kind:/ { kind = $2 }
  /^metadata:/ { inmeta = 1; next }
  inmeta && /^  name:/ { name = $2; gsub(/"/, "", name); if (kind != "") print kind "/" name; inmeta = 0 }
  /^[a-zA-Z]/ && !/^metadata:/ { inmeta = 0 }
' | grep -E "^(${cluster_scoped})/" | sort -u || true)
expected=$(printf '%s\n' "$@" | grep -v '^$' | sort -u || true)

if [ "$found" != "$expected" ]; then
  echo "::error::cluster-scoped objects differ from the expected set" >&2
  diff <(echo "$expected") <(echo "$found") >&2 || true
  exit 1
fi
echo "cluster-scoped objects: ${found:-none}"
