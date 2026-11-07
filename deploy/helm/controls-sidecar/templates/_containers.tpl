{{/* Containers injected into a service Deployment. */}}
{{- define "controls-sidecar.containers" -}}
- name: controls-envoy
  image: {{ required "envoy.image is required" .Values.envoy.image | quote }}
  imagePullPolicy: {{ .Values.envoy.pullPolicy | default "IfNotPresent" }}
  args:
    - -c
    - /etc/controls/envoy/envoy.yaml
    - --service-cluster
    - {{ .Values.cluster | default "app" | quote }}
    - --service-node
    - $(POD_NAME)
  env:
    - name: POD_NAME
      valueFrom:
        fieldRef:
          fieldPath: metadata.name
  ports:
    - name: http
      containerPort: {{ .Values.envoy.port | default 8080 }}
    - name: admin
      containerPort: {{ .Values.envoy.adminPort | default 9901 }}
  volumeMounts:
    - name: controls-envoy-config
      mountPath: /etc/controls/envoy
      readOnly: true
  resources:
    {{- toYaml (.Values.envoy.resources | default dict) | nindent 4 }}
- name: controls-extproc
  image: {{ required "extproc.image is required" .Values.extproc.image | quote }}
  imagePullPolicy: {{ .Values.extproc.pullPolicy | default "IfNotPresent" }}
  args:
    - -listen
    - ":{{ .Values.extproc.port | default 50051 }}"
    - -config
    - /etc/controls/bundle
    - -pip
    - {{ required "extproc.pipURL is required" .Values.extproc.pipURL | quote }}
    - -metrics-addr
    - ":{{ .Values.extproc.metricsPort | default 9090 }}"
    - -audit-sample
    - {{ .Values.extproc.auditSample | quote }}
    - -audit-buffer
    - {{ .Values.extproc.auditBuffer | quote }}
    {{- if .Values.extproc.auditOut }}
    - -audit-out
    - {{ .Values.extproc.auditOut | quote }}
    {{- end }}
    {{- if .Values.extproc.pipCA }}
    - -pip-ca
    - /etc/controls/secrets/{{ .Values.extproc.pipCA }}
    {{- end }}
    {{- if .Values.extproc.pipCert }}
    - -pip-cert
    - /etc/controls/secrets/{{ .Values.extproc.pipCert }}
    {{- end }}
    {{- if .Values.extproc.pipKey }}
    - -pip-key
    - /etc/controls/secrets/{{ .Values.extproc.pipKey }}
    {{- end }}
    {{- if .Values.extproc.pipServerName }}
    - -pip-server-name
    - {{ .Values.extproc.pipServerName | quote }}
    {{- end }}
    {{- if .Values.extproc.allowUnsigned }}
    - -allow-unsigned
    {{- else }}
    - -bundle-pubkey
    - /etc/controls/secrets/{{ .Values.secrets.bundlePubKey }}
    {{- end }}
  env:
    - name: CONTROLS_AUDIT_SALT
      valueFrom:
        secretKeyRef:
          name: {{ .Values.volumes.secrets }}
          key: {{ .Values.secrets.auditSaltKey }}
    - name: CONTROLS_IDENTITY_HMAC_KEY
      valueFrom:
        secretKeyRef:
          name: {{ .Values.volumes.secrets }}
          key: {{ .Values.secrets.identityKey }}
  ports:
    - name: extproc
      containerPort: {{ .Values.extproc.port | default 50051 }}
    - name: metrics
      containerPort: {{ .Values.extproc.metricsPort | default 9090 }}
  volumeMounts:
    - name: controls-bundle
      mountPath: /etc/controls/bundle
      readOnly: true
    - name: controls-secrets
      mountPath: /etc/controls/secrets
      readOnly: true
  resources:
    {{- toYaml (.Values.extproc.resources | default dict) | nindent 4 }}
{{- end }}
