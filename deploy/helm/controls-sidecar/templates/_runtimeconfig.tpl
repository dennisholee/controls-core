{{/* Literal sidecar.yaml generated from values. Included by the parent when
     extproc.runtimeConfig is true. */}}
{{- define "controls-sidecar.runtimeConfig" -}}
server:
  listen: ":{{ .Values.extproc.port | default 50051 }}"
  metricsAddr: ":{{ .Values.extproc.metricsPort | default 9090 }}"
bundle:
  path: /etc/controls/bundle
  {{- if .Values.extproc.allowUnsigned }}
  allowUnsigned: true
  {{- else }}
  pubKey: /etc/controls/secrets/{{ .Values.secrets.bundlePubKey }}
  {{- end }}
  reloadInterval: {{ .Values.extproc.reloadInterval | default "30s" | quote }}
pip:
  apiVersion: {{ .Values.extproc.pipAPIVersion | default "1.0" | quote }}
  url: {{ required "extproc.pipURL is required" .Values.extproc.pipURL | quote }}
  timeout: {{ .Values.extproc.pipTimeout | default "100ms" | quote }}
  defaultTTL: {{ .Values.extproc.pipDefaultTTL | default "30s" | quote }}
  maxTTL: {{ .Values.extproc.maxTTL | default "5m" | quote }}
  maxCache: {{ .Values.extproc.pipMaxCache | default 100000 }}
  breakerCooldown: {{ .Values.extproc.pipBreakerCooldown | default "5s" | quote }}
  {{- if .Values.extproc.pipCA }}
  tls:
    ca: /etc/controls/secrets/{{ .Values.extproc.pipCA }}
    cert: /etc/controls/secrets/{{ .Values.extproc.pipCert }}
    key: /etc/controls/secrets/{{ .Values.extproc.pipKey }}
    serverName: {{ .Values.extproc.pipServerName | default "pip" | quote }}
  {{- end }}
identity:
  hmacKey: env:CONTROLS_IDENTITY_HMAC_KEY
  maxSkew: {{ .Values.extproc.identityMaxSkew | default "1m" | quote }}
audit:
  sample: {{ .Values.extproc.auditSample | default "0.01" }}
  buffer: {{ .Values.extproc.auditBuffer | default 4096 }}
  {{- if .Values.extproc.auditOut }}
  out: {{ .Values.extproc.auditOut | quote }}
  {{- end }}
  salt: env:CONTROLS_AUDIT_SALT
{{- if .Values.extproc.opaURL }}
policy:
  opa:
    url: {{ .Values.extproc.opaURL | quote }}
    timeout: {{ .Values.extproc.opaTimeout | default "100ms" | quote }}
{{- end }}
{{- end }}

{{- define "controls-sidecar.configmap" -}}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Values.extproc.runtimeConfigMap | default "controls-runtime-config" }}
data:
  sidecar.yaml: |
{{ include "controls-sidecar.runtimeConfig" . | indent 4 }}
{{- end }}
