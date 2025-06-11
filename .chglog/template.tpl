# История изменений

{{ range .Versions }}
## {{ .Tag.Name }} ({{ datetime "2006-01-02" .Tag.Date }})

{{- range .Commits }}
- [{{ .Header }}](https://github.com/mkostelcev/nexus-operator/commit/{{ .Hash }})
{{- end }}

{{ end }}

