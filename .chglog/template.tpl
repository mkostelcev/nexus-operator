# История изменений

{{ range .Versions }}
## {{ .Tag.Name }} ({{ datetime "2006-01-02" .Tag.Date }})

--- 

### 📦 Docker-образ

[ghcr.io/mkostelcev/nexus-operator:{{ .Tag.Name }}](https://github.com/orgs/mkostelcev/packages/container/nexus-operator)

```sh
docker pull ghcr.io/mkostelcev/nexus-operator:{{ .Tag.Name }}
```

{{- range .Commits }}
- [{{ .Header }}](https://github.com/mkostelcev/nexus-operator/commit/{{ .Hash }})
{{- end }}

{{ end }}

