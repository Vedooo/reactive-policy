{{/*
Expand the name of the chart.
*/}}
{{- define "reactive-policy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "reactive-policy.fullname" -}}
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
Chart name and version label.
*/}}
{{- define "reactive-policy.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "reactive-policy.labels" -}}
helm.sh/chart: {{ include "reactive-policy.chart" . }}
{{ include "reactive-policy.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: reactive-policy
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "reactive-policy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "reactive-policy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
ServiceAccount name to use.
*/}}
{{- define "reactive-policy.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "reactive-policy.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Container image reference, defaulting the tag to the chart appVersion.
*/}}
{{- define "reactive-policy.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end }}

{{/*
Name of the Secret holding the webhook serving certificate. cert-manager writes
it from the Certificate; a bring-your-own setup names an existing one.
*/}}
{{- define "reactive-policy.webhookCertSecret" -}}
{{- if .Values.webhook.existingSecret -}}
{{- .Values.webhook.existingSecret -}}
{{- else -}}
{{- printf "%s-webhook-cert" (include "reactive-policy.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Base name for the webhook configurations. These are cluster-scoped, so the
release namespace is folded in to keep two releases in different namespaces from
colliding on the same object.
*/}}
{{- define "reactive-policy.webhookName" -}}
{{- printf "%s-%s" (include "reactive-policy.fullname" .) .Release.Namespace | trunc 63 | trimSuffix "-" -}}
{{- end }}
