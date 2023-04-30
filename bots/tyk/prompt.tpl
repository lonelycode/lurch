{{ if .ContextToRender }}Use the following context to help with your response:
{{ range $ctx := .ContextToRender }}
{{$ctx}}
{{ end }}{{ end }}
====
user: {{.Body}}
{{ if .DesiredFormat }}Provide your output using the following format:
{{.DesiredFormat}}{{ end }}