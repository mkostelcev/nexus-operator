package importer

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

// knownFormats — форматы, поддерживаемые NexusTeamAccess.
var knownFormats = []string{"docker", "maven2", "raw", "npm", "nuget"}

// accessLevelActions определяет соответствие actions → accessLevel.
var accessLevelDefs = map[string][]string{
	"ro":  {"BROWSE", "READ"},
	"rw":  {"ADD", "BROWSE", "EDIT", "READ"},
	"rwd": {"ADD", "BROWSE", "DELETE", "EDIT", "READ"},
}

// teamAccessBuilder собирает данные для одного NexusTeamAccess CR.
type teamAccessBuilder struct {
	teamPath     string
	formats      map[string]map[string]bool // format → set of repo names
	accessLevels map[string]bool
}

func (imp *Importer) importNexusTeamAccesses(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("nexus-team-access")
	log.Info("Импорт NexusTeamAccess")

	privs, err := imp.nexusClient.ListPrivileges(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка привилегий")
		return 0, 0, 1
	}

	// Группируем по csPrefix (cs-{teamName})
	builders := make(map[string]*teamAccessBuilder)

	for _, priv := range privs {
		if priv.Type != "repository-content-selector" || priv.ReadOnly {
			continue
		}

		data, err := imp.nexusClient.GetPrivilege(ctx, priv.Name)
		if err != nil {
			log.Error(err, "Ошибка получения данных привилегии", "name", priv.Name)
			errs++
			continue
		}

		csName := getStringField(data, "contentSelector")
		format := getStringField(data, "format")
		repository := getStringField(data, "repository")
		actions := getStringSliceField(data, "actions")

		if csName == "" || format == "" || repository == "" {
			skipped++
			continue
		}

		teamName, parsedFormat, _, ok := parseCSContentSelectorName(csName)
		if !ok {
			skipped++
			continue
		}

		// Формат из имени CS должен совпадать с форматом привилегии
		if parsedFormat != format {
			skipped++
			continue
		}

		level := actionsToAccessLevel(actions)
		if level == "" {
			skipped++
			continue
		}

		csPrefix := "cs-" + teamName
		b, exists := builders[csPrefix]
		if !exists {
			b = &teamAccessBuilder{
				formats:      make(map[string]map[string]bool),
				accessLevels: make(map[string]bool),
			}
			builders[csPrefix] = b
		}

		if b.formats[format] == nil {
			b.formats[format] = make(map[string]bool)
		}
		b.formats[format][repository] = true
		b.accessLevels[level] = true
	}

	// Для каждой группы восстанавливаем teamPath из CSEL expression первого CS
	for csPrefix, b := range builders {
		teamName := strings.TrimPrefix(csPrefix, "cs-")

		// Берём первый формат/репо для получения CS expression
		var firstFormat string
		var firstCSName string
		for format, repos := range b.formats {
			for repo := range repos {
				firstFormat = format
				repoSanitized := utils.SanitizeK8sName(repo)
				firstCSName = "cs-" + teamName + "-" + format + "-" + repoSanitized
				break
			}
			break
		}

		if firstCSName == "" {
			skipped++
			continue
		}

		cs, err := imp.nexusClient.GetContentSelector(ctx, firstCSName)
		if err != nil {
			log.V(1).Info("Не удалось получить CS для восстановления teamPath, используем teamName",
				"csName", firstCSName, "teamName", teamName)
			// Fallback: используем teamName как teamPath (заменяя - на /)
			b.teamPath = strings.ReplaceAll(teamName, "-", "/")
		} else {
			teamPath := extractTeamPathFromCSEL(cs.Expression, firstFormat)
			if teamPath == "" {
				b.teamPath = strings.ReplaceAll(teamName, "-", "/")
			} else {
				b.teamPath = teamPath
			}
		}
	}

	// Создаём NexusTeamAccess CR для каждой группы
	for csPrefix, b := range builders {
		teamName := strings.TrimPrefix(csPrefix, "cs-")

		// Собираем repositories
		var repositories []v1alpha1.RepositoryGroup
		// Сортируем форматы для детерминированного вывода
		formats := make([]string, 0, len(b.formats))
		for f := range b.formats {
			formats = append(formats, f)
		}
		sort.Strings(formats)

		for _, format := range formats {
			repos := b.formats[format]
			names := make([]string, 0, len(repos))
			for name := range repos {
				names = append(names, name)
			}
			sort.Strings(names)
			repositories = append(repositories, v1alpha1.RepositoryGroup{
				Format: format,
				Names:  names,
			})
		}

		// Собираем accessLevels
		levels := make([]string, 0, len(b.accessLevels))
		for l := range b.accessLevels {
			levels = append(levels, l)
		}
		sort.Strings(levels)

		k8sName := utils.SanitizeK8sName("nta-" + teamName)

		cr := &v1alpha1.NexusTeamAccess{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: map[string]string{
					annotationNexusName: b.teamPath,
				},
			},
			Spec: v1alpha1.NexusTeamAccessSpec{
				TeamPath:     b.teamPath,
				Repositories: repositories,
				AccessLevels: levels,
			},
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание NexusTeamAccess",
				"name", k8sName, "teamPath", b.teamPath)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				log.V(1).Info("NexusTeamAccess уже существует, пропуск", "name", k8sName)
				skipped++
			} else {
				log.Error(err, "Ошибка создания NexusTeamAccess", "name", k8sName)
				errs++
			}
		} else {
			log.Info("NexusTeamAccess создан", "name", k8sName, "teamPath", b.teamPath)
			created++
		}
	}

	return created, skipped, errs
}

// parseCSContentSelectorName парсит имя CS по паттерну cs-{teamName}-{format}-{repo}.
// Формат ищем справа налево, потому что teamName может содержать дефисы.
func parseCSContentSelectorName(name string) (teamName, format, repo string, ok bool) {
	if !strings.HasPrefix(name, "cs-") {
		return "", "", "", false
	}
	rest := name[3:] // убираем "cs-"

	// Ищем формат: находим первое вхождение "-{format}-"
	// Index (а не LastIndex), чтобы корректно обрабатывать repo вида "npm-hosted"
	for _, f := range knownFormats {
		sep := "-" + f + "-"
		idx := strings.Index(rest, sep)
		if idx < 0 {
			continue
		}
		teamName = rest[:idx]
		repo = rest[idx+len(sep):]
		if teamName == "" || repo == "" {
			continue
		}
		return teamName, f, repo, true
	}
	return "", "", "", false
}

// actionsToAccessLevel определяет accessLevel по набору actions.
func actionsToAccessLevel(actions []string) string {
	sorted := make([]string, len(actions))
	copy(sorted, actions)
	sort.Strings(sorted)
	key := strings.Join(sorted, ",")

	for level, levelActions := range accessLevelDefs {
		sortedLevel := make([]string, len(levelActions))
		copy(sortedLevel, levelActions)
		sort.Strings(sortedLevel)
		if key == strings.Join(sortedLevel, ",") {
			return level
		}
	}
	return ""
}

// extractTeamPathFromCSEL извлекает teamPath из CSEL expression по формату.
func extractTeamPathFromCSEL(expression, format string) string {
	switch format {
	case "docker":
		// path =~ "^/v2/(team/path/.*)?$"
		re := regexp.MustCompile(`\^/v2/\((.+)/\.\*\)\?\$`)
		m := re.FindStringSubmatch(expression)
		if len(m) >= 2 {
			return m[1]
		}
	case "maven2":
		// path =~ "^/ru/(team/path/.*)?$"
		re := regexp.MustCompile(`\^/ru/\((.+)/\.\*\)\?\$`)
		m := re.FindStringSubmatch(expression)
		if len(m) >= 2 {
			return m[1]
		}
	case "raw":
		// path =~ "^/(team/path/.*)?$"
		re := regexp.MustCompile(`\^/\((.+)/\.\*\)\?\$`)
		m := re.FindStringSubmatch(expression)
		if len(m) >= 2 {
			return m[1]
		}
	case "npm":
		// path =~ "^@team.path.*$"
		re := regexp.MustCompile(`\^@(.+)\.\*\$`)
		m := re.FindStringSubmatch(expression)
		if len(m) >= 2 {
			return strings.ReplaceAll(m[1], ".", "/")
		}
	case "nuget":
		// path =~ "^team.path.*$"
		re := regexp.MustCompile(`\^([^@/].+)\.\*\$`)
		m := re.FindStringSubmatch(expression)
		if len(m) >= 2 {
			return strings.ReplaceAll(m[1], ".", "/")
		}
	}
	return ""
}

func getStringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringSliceField(m map[string]any, key string) []string {
	if arr, ok := m[key].([]any); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
