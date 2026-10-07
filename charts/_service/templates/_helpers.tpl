{{/*
Fully qualified app name for one alias of this chart, e.g. "steward-core".
*/}}
{{- define "service-chart.fullname" -}}
steward-{{ .Values.aliasName | default .Chart.Name }}
{{- end -}}

{{/*
Standard labels.
*/}}
{{- define "service-chart.labels" -}}
app.kubernetes.io/name: {{ include "service-chart.fullname" . }}
app.kubernetes.io/part-of: steward
app.kubernetes.io/managed-by: Helm
{{- end -}}

{{/*
Selector labels (stable across releases; never add the chart version here).
*/}}
{{- define "service-chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "service-chart.fullname" . }}
{{- end -}}

{{/*
The service account name, whether created by this chart or supplied by the operator.
*/}}
{{- define "service-chart.serviceAccountName" -}}
{{ .Values.serviceAccount.name | default (include "service-chart.fullname" .) }}
{{- end -}}

{{/*
WORKLOAD_ALLOWED_SERVICEACCOUNTS: the alias's caller list, as a comma-joined
"<namespace>/steward-<caller>" string. An empty namespace in the values list
resolves to the release namespace.
*/}}
{{- define "service-chart.allowedServiceAccounts" -}}
{{- $ns := .Release.Namespace -}}
{{- $items := list -}}
{{- range .Values.workloadAuth.allowedServiceAccounts -}}
{{- $items = append $items (printf "%s/steward-%s" $ns .) -}}
{{- end -}}
{{- join "," $items -}}
{{- end -}}

{{/*
The probe type: "http" (GET /readyz and /livez on probePort) or "grpc" (the
kubelet's native grpc.health.v1 probe on the main port). Anything else fails
the render.
*/}}
{{- define "service-chart.probeType" -}}
{{- $t := .Values.probe.type | default "http" -}}
{{- if not (has $t (list "http" "grpc")) -}}
{{- fail (printf "%s: probe.type must be http or grpc, got %q" (include "service-chart.fullname" .) $t) -}}
{{- end -}}
{{- $t -}}
{{- end -}}

{{/*
"true" when the service needs a separate probe port: an HTTP probe on a port
other than the main one. An HTTP probe on the main port, or a gRPC probe,
reuses the main port and renders no second port entry.
*/}}
{{- define "service-chart.separateProbePort" -}}
{{- if and (eq (include "service-chart.probeType" .) "http") (ne (int .Values.probePort) (int .Values.port.number)) -}}
true
{{- end -}}
{{- end -}}

{{/*
Every container port this alias renders: the main port, the separate probe
port when there is one, then extraPorts. Fails the render when a number or a
name repeats, so two listeners can never be configured onto one port.
*/}}
{{- define "service-chart.containerPorts" -}}
{{- $ports := list (dict "name" .Values.port.name "number" (int .Values.port.number)) -}}
{{- if include "service-chart.separateProbePort" . -}}
{{- $ports = append $ports (dict "name" "probe" "number" (int .Values.probePort)) -}}
{{- end -}}
{{- range .Values.extraPorts -}}
{{- $ports = append $ports (dict "name" .name "number" (int .number)) -}}
{{- end -}}
{{- $numbers := list -}}
{{- $names := list -}}
{{- range $ports -}}
{{- if has .number $numbers -}}
{{- fail (printf "%s: container port %d is used more than once" (include "service-chart.fullname" $) .number) -}}
{{- end -}}
{{- if has .name $names -}}
{{- fail (printf "%s: container port name %q is used more than once" (include "service-chart.fullname" $) .name) -}}
{{- end -}}
{{- $numbers = append $numbers .number -}}
{{- $names = append $names .name -}}
{{- end -}}
{{- toJson $ports -}}
{{- end -}}

{{/*
workloadAuth.authMode, validated: "enabled" (WORKLOAD_AUTH left unset, the
verification block set) or "disabled" (WORKLOAD_AUTH=disabled alone, local
runs only). The services accept nothing else.
*/}}
{{- define "service-chart.workloadAuthMode" -}}
{{- $m := .Values.workloadAuth.authMode | default "enabled" -}}
{{- if not (has $m (list "enabled" "disabled")) -}}
{{- fail (printf "%s: workloadAuth.authMode must be enabled or disabled, got %q" (include "service-chart.fullname" .) $m) -}}
{{- end -}}
{{- $m -}}
{{- end -}}

{{/*
"true" for an enabled callee: it fetches the issuer's JWKS and gets the
second (default-audience) token and the cluster CA mounted for that.
*/}}
{{- define "service-chart.jwksCredentials" -}}
{{- if and .Values.workloadAuth.callee (eq (include "service-chart.workloadAuthMode" .) "enabled") -}}
true
{{- end -}}
{{- end -}}
