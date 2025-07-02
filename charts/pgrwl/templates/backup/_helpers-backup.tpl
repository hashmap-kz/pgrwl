{{- define "pgrwl.backupFullname" -}}
{{ include "pgrwl.fullname" . }}-backup
{{- end }}

{{- define "pgrwl.backupLabels" -}}
{{ include "pgrwl.labels" . }}
app.kubernetes.io/component: backup
{{- end }}

{{- define "pgrwl.backupSelectorLabels" -}}
{{ include "pgrwl.selectorLabels" . }}
app.kubernetes.io/component: backup
{{- end }}
