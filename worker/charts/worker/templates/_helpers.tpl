{{/*
Expand the name of the chart.
*/}}
{{- define "husonym-worker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "husonym-worker.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "husonym-worker.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "husonym-worker.labels" -}}
helm.sh/chart: {{ include "husonym-worker.chart" . }}
{{ include "husonym-worker.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "husonym-worker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "husonym-worker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "husonym-worker.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "husonym-worker.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Generate the stringData section for environment variables
*/}}
{{- define "husonym-worker.env-vars" -}}
{{- if .Values.host }}
HOST: {{ .Values.host | quote}}
{{- end }}
{{- if .Values.containerPort }}
PORT: {{ .Values.containerPort | quote }}
{{- end }}
{{- if .Values.otel.enabled }}
OTEL_EXPORTER_OTLP_PORT: {{ .Values.otel.otlpPort | quote }} # sends to gRPC receiver
{{- end }}
{{- if .Values.nucleusEnv }}
NUCLEUS_ENV: {{ .Values.nucleusEnv }}
{{- end }}
{{- if .Values.temporal.url }}
TEMPORAL_URL: {{ .Values.temporal.url }}
{{- end }}
{{- if .Values.temporal.namespace }}
TEMPORAL_NAMESPACE: {{ .Values.temporal.namespace }}
{{- end }}
{{- if .Values.temporal.taskQueue }}
TEMPORAL_TASK_QUEUE: {{ .Values.temporal.taskQueue }}
{{- end }}
{{- if and .Values.temporal .Values.temporal.certificate .Values.temporal.certificate.keyFilePath }}
TEMPORAL_CERT_KEY_PATH: {{ .Values.temporal.certificate.keyFilePath }}
{{- end }}
{{- if and .Values.temporal .Values.temporal.certificate .Values.temporal.certificate.certFilePath }}
TEMPORAL_CERT_PATH: {{ .Values.temporal.certificate.certFilePath }}
{{- end }}
{{- if and .Values.temporal .Values.temporal.certificate .Values.temporal.certificate.keyContents }}
TEMPORAL_CERT_KEY: {{ .Values.temporal.certificate.keyContents }}
{{- end }}
{{- if and .Values.temporal .Values.temporal.certificate .Values.temporal.certificate.certContents }}
TEMPORAL_CERT: {{ .Values.temporal.certificate.certContents }}
{{- end }}
{{- if and .Values.husonym .Values.husonym.url }}
HUSONYM_URL: {{ .Values.husonym.url }}
{{- end }}
{{- if and .Values.husonym .Values.husonym.apiKey }}
HUSONYM_API_KEY: {{ .Values.husonym.apiKey }}
{{- end }}
{{- if .Values.redis.url }}
REDIS_URL: {{ .Values.redis.url }}
{{- end }}
{{- if .Values.redis.kind }}
REDIS_KIND: {{ .Values.redis.kind }}
{{- end }}
{{- if .Values.redis.master }}
REDIS_MASTER: {{ .Values.redis.master }}
{{- end }}
REDIS_TLS_ENABLED: {{ .Values.redis.tls.enabled | default "false" | quote }}
REDIS_TLS_SKIP_CERT_VERIFY: {{ .Values.redis.tls.skipCertVerify | default "false" | quote }}
REDIS_TLS_ENABLE_RENEGOTIATION: {{ .Values.redis.tls.enableRenegotiation | default "false" | quote }}
{{- if and .Values.redis .Values.redis.tls .Values.redis.tls.rootCertAuthority }}
REDIS_TLS_ROOT_CERT_AUTHORITY: {{ .Values.redis.tls.rootCertAuthority }}
{{- end }}
{{- if and .Values.redis .Values.redis.tls .Values.redis.tls.rootCertAuthorityFile }}
REDIS_TLS_ROOT_CERT_AUTHORITY_FILE: {{ .Values.redis.tls.rootCertAuthorityFile }}
{{- end }}
HUSONYM_CLOUD: {{ .Values.husonymCloud.enabled | default "false" | quote }}
{{- if and .Values.ee .Values.ee.license }}
EE_LICENSE: {{ .Values.ee.license | quote }}
{{- end }}
TABLESYNC_MAX_CONCURRENCY: {{ .Values.tableSync.maxConcurrency | quote }}
{{- if and .Values.llm .Values.llm.enabled }}
{{- if .Values.llm.baseUrl }}
LLM_BASE_URL: {{ .Values.llm.baseUrl }}
{{- end }}
{{- if .Values.llm.apiKey }}
LLM_API_KEY: {{ .Values.llm.apiKey }}
{{- end }}
{{- if .Values.llm.model }}
LLM_MODEL: {{ .Values.llm.model }}
{{- end }}
{{- end }}
{{- end -}}
