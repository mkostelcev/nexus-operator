package utils

import (
	"regexp"
	"strings"
)

// ContainsString проверяет, содержится ли строка в срезе.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// RemoveString удаляет строку из среза.
func RemoveString(slice []string, s string) []string {
	result := []string{}
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

var invalidK8sChars = regexp.MustCompile(`[^a-z0-9\-]`)
var multiDash = regexp.MustCompile(`-{2,}`)

// SanitizeK8sName конвертирует имя Nexus-ресурса в валидное имя K8s-ресурса (RFC 1123).
func SanitizeK8sName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = invalidK8sChars.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 253 {
		s = s[:253]
		s = strings.TrimRight(s, "-")
	}
	return s
}
