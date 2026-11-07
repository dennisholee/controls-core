{{- define "controls-plane.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "controls-plane.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "controls-plane.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "controls-plane.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "controls-plane.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: pip
{{- end }}

{{- define "controls-plane.selectorLabels" -}}
app.kubernetes.io/name: {{ include "controls-plane.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: pip
{{- end }}

{{- define "controls-plane.pipNamespace" -}}
{{- default .Release.Namespace .Values.networkPolicy.sidecar.pipNamespace }}
{{- end }}
