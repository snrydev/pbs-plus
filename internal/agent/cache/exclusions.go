//go:build windows

package cache

import (
	"net/http"
	"strings"

	"github.com/sonroyaalmerol/pbs-plus/internal/agent"
	"github.com/sonroyaalmerol/pbs-plus/internal/syslog"
	"github.com/sonroyaalmerol/pbs-plus/internal/utils/pattern"
)

type ExclusionData struct {
	Path    string `json:"path"`
	Comment string `json:"comment"`
}

type ExclusionResp struct {
	Data []ExclusionData `json:"data"`
}

func CompileExcludedPaths() []*pattern.Pattern {
	var exclusionResp ExclusionResp
	_, err := agent.ProxmoxHTTPRequest(
		http.MethodGet,
		"/api2/json/d2d/exclusion",
		nil,
		&exclusionResp,
	)
	if err != nil {
		exclusionResp = ExclusionResp{
			Data: []ExclusionData{},
		}
	}

	excludedPatterns := []string{}

	for _, userExclusions := range exclusionResp.Data {
		trimmedLine := strings.TrimSpace(userExclusions.Path)
		excludedPatterns = append(excludedPatterns, trimmedLine)
	}

	syslog.L.Infof("Retrieved exclusions: %v", excludedPatterns)

	var compiledPatterns []*pattern.Pattern

	// Compile excluded patterns
	for _, patternStr := range excludedPatterns {
		ptrn, err := pattern.NewPattern(patternStr)
		if err != nil {
			continue
		}
		compiledPatterns = append(compiledPatterns, ptrn)
	}

	return compiledPatterns
}
