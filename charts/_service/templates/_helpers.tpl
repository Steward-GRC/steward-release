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
{{- if .Values.serviceAccount.create -}}
{{ include "service-chart.fullname" . }}
{{- else -}}
{{ .Values.serviceAccount.name | default (include "service-chart.fullname" .) }}
{{- end -}}
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
