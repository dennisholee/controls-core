{{/* Volumes referenced by the injected containers. */}}
{{- define "controls-sidecar.volumes" -}}
{{- if .Values.extproc.runtimeConfig }}
- name: controls-runtime-config
  configMap:
    name: {{ .Values.extproc.runtimeConfigMap | default "controls-runtime-config" }}
{{- end }}
- name: controls-envoy-config
  configMap:
    name: {{ required "volumes.envoyConfig is required" .Values.volumes.envoyConfig }}
- name: controls-bundle
  configMap:
    name: {{ required "volumes.bundle is required" .Values.volumes.bundle }}
- name: controls-secrets
  secret:
    secretName: {{ required "volumes.secrets is required" .Values.volumes.secrets }}
{{- end }}
