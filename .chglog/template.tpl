# История изменений

{{ range .Versions }}
## {{ .Tag.Name }} {{ if .Tag.Date }}({{ datetime "2006-01-02" .Tag.Date }}){{ end }}

{{- range .Commits }}
  - {{ .Header }}
{{- end }}

{{ end }}
