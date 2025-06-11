# История изменений

{{ range .Versions }}
## {{ .Tag.Name }}

{{ .Subject | indent 2 }}

{{ end }}
