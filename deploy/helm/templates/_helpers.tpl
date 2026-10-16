{{- define "pulse.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}{{ .Release.Name | trunc 50 | trimSuffix "-" }}{{- else -}}{{ printf "%s-%s" .Release.Name .Chart.Name | trunc 50 | trimSuffix "-" }}{{- end -}}
{{- end -}}

{{- define "pulse.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
app.kubernetes.io/part-of: pulse
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "pulse.selector" -}}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/name: {{ .name }}
{{- end -}}

{{- define "pulse.image" -}}
{{ printf "%s/%s%s:%s" .root.Values.image.registry .root.Values.image.prefix .name (default .root.Chart.AppVersion .root.Values.image.tag) }}
{{- end -}}

{{- define "pulse.secretName" -}}
{{ default (printf "%s-secrets" (include "pulse.fullname" .)) .Values.existingSecret }}
{{- end -}}

{{/* Components: values key, image/name, metrics port (0 = health port). */}}
{{- define "pulse.components" -}}
ingest-gateway: ingestGateway
stream-processor: streamProcessor
query-api: queryApi
alerting-worker: alertingWorker
governance-worker: governanceWorker
web: web
{{- end -}}
