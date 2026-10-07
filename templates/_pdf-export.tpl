{{- /*
delivery's effective entry for one environment variable, as JSON: its `env`
entry when there is one (an operator override), else its `baseEnv` entry,
else {}.
*/ -}}
{{- define "steward.deliveryEnvEntry" -}}
{{- $name := .name -}}
{{- $d := .delivery -}}
{{- $out := dict -}}
{{- $found := false -}}
{{- range (concat ($d.env | default list) ($d.baseEnv | default list)) -}}
{{- if and (not $found) (eq .name $name) -}}
{{- $out = . -}}
{{- $found = true -}}
{{- end -}}
{{- end -}}
{{- toJson $out -}}
{{- end -}}

{{- /*
"true" while delivery runs with PDF export on: PDF_EXPORT_ENABLED unset (the
service's default) or anything but "false".
*/ -}}
{{- define "steward.pdfExportOn" -}}
{{- $d := .Values.delivery | default dict -}}
{{- if $d.enabled -}}
{{- $e := include "steward.deliveryEnvEntry" (dict "delivery" $d "name" "PDF_EXPORT_ENABLED") | fromJson -}}
{{- if ne (toString ($e.value | default "") | lower) "false" -}}
true
{{- end -}}
{{- end -}}
{{- end -}}

{{- /*
The Secret the render Jobs load their object storage from: pdf-renderer's
S3_SECRET_NAME (its `env` entry over its `baseEnv` one).
*/ -}}
{{- define "steward.pdfRendererS3Secret" -}}
{{- $out := "steward-pdf-renderer-s3" -}}
{{- $p := index .Values "pdf-renderer" | default dict -}}
{{- range (concat ($p.baseEnv | default list) ($p.env | default list)) -}}
{{- if eq .name "S3_SECRET_NAME" -}}
{{- $out = .value -}}
{{- end -}}
{{- end -}}
{{- $out -}}
{{- end -}}
