package controller

// Хелперы для информативного логирования расхождений desired/current.
// В лог попадают только пути полей, значения не раскрываются —
// пароли и токены утечь не могут by construction.

import (
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
)

// diffPathReporter собирает пути отличий при сравнении через cmp.
type diffPathReporter struct {
	path  cmp.Path
	paths []string
}

func (r *diffPathReporter) PushStep(ps cmp.PathStep) { r.path = append(r.path, ps) }
func (r *diffPathReporter) PopStep()                 { r.path = r.path[:len(r.path)-1] }
func (r *diffPathReporter) Report(rs cmp.Result) {
	if !rs.Equal() {
		r.paths = append(r.paths, formatDiffPath(r.path))
	}
}

// formatDiffPath печатает путь в компактном виде:
// httpClient.authentication.password, group.memberNames[2].
func formatDiffPath(path cmp.Path) string {
	var sb strings.Builder
	for _, step := range path {
		switch s := step.(type) {
		case cmp.MapIndex:
			fmt.Fprintf(&sb, ".%v", s.Key())
		case cmp.StructField:
			fmt.Fprintf(&sb, ".%s", s.Name())
		case cmp.SliceIndex:
			fmt.Fprintf(&sb, "[%d]", s.Key())
		}
	}
	result := strings.TrimPrefix(sb.String(), ".")
	if result == "" {
		return "<корень>"
	}
	return result
}

// diffPaths возвращает пути, по которым desired и current различаются.
func diffPaths(desired, current interface{}, opts ...cmp.Option) []string {
	reporter := &diffPathReporter{}
	opts = append(opts, cmp.Reporter(reporter))
	cmp.Equal(desired, current, opts...)
	return reporter.paths
}
