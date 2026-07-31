{{- define "cnpg-console.name" -}}cnpg-console{{- end -}}

{{- define "cnpg-console.labels" -}}
app.kubernetes.io/name: cnpg-console
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "cnpg-console.selectorLabels" -}}
app.kubernetes.io/name: cnpg-console
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cnpg-console.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end -}}

{{- define "cnpg-console.secretName" -}}
{{- if .Values.secret.existingSecret -}}{{ .Values.secret.existingSecret }}{{- else -}}cnpg-console-secrets{{- end -}}
{{- end -}}
