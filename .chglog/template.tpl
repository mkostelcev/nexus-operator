# История изменений

{{ range .Versions }}
## {{ .Tag.Name }} ({{ datetime "2006-01-02" .Tag.Date }})

{{- range .Commits }}
- [{{ .Header }}]({{ $.RepositoryURL }}/commit/{{ .Hash }})
{{- end }}

{{ end }}

