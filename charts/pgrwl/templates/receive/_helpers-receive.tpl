{{- define "pgrwl.receiveFullname" -}}
{{ include "pgrwl.fullname" . }}-receive
{{- end }}

{{- define "pgrwl.receiveLabels" -}}
{{ include "pgrwl.labels" . }}
app.kubernetes.io/component: receive
{{- end }}

{{- define "pgrwl.receiveSelectorLabels" -}}
{{ include "pgrwl.selectorLabels" . }}
app.kubernetes.io/component: receive
{{- end }}
