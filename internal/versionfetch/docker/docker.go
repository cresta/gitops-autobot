package docker

import (
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/go-logfmt/logfmt"
	"strconv"
	"strings"
)


type UpgradeInfo struct {
	Registry          string
	Repository        string
	ChartName         string
	CurrentVersion    string
	VersionConstraint string
	AutoMerge         *bool
	AutoApprove       *bool
}

type LineDockerChange struct {
	UpgradeInfo              UpgradeInfo
	CurrentVersionLine       string
	CurrentVersionLineNumber int
}

// ParseDockerYAML will inspect a YAML formatted file for docker upgrade directives.
func ParseDockerYAML(lines []string) ([]*LineDockerChange, error) {
	ret := make([]*LineDockerChange, 0, 3) // most files will have only a few changes.
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		gitopsStart := strings.LastIndex(trimmed, autobotPrefix)
		if gitopsStart == -1 {
			continue
		}
		trimmed = trimmed[gitopsStart+len(autobotPrefix):]
		trimmed = strings.TrimSpace(trimmed)
		dec := logfmt.NewDecoder(strings.NewReader(trimmed))
		keys := make(map[string]string)
		for dec.ScanRecord() {
			for dec.ScanKeyval() {
				keys[string(dec.Key())] = string(dec.Value())
			}
		}
		if keys["changer"] != "helm" {
			continue
		}
		lineIndent := yamlIndent(line)
		// Look in the next 3 lines for information
		if idx+3 >= len(lines) {
			continue
		}
		var thisChange LineHelmChange
		for idx2 := idx + 1; idx2 <= idx+3; idx2++ {
			if yamlIndent(lines[idx2]) != lineIndent {
				continue
			}
			v := make(map[string]string)
			if err := yaml.Unmarshal([]byte(lines[idx2]), &v); err != nil {
				continue
			}
			if repoStr, exists := v["repository"]; exists {
				thisChange.UpgradeInfo.Repository = repoStr
			}
			if name, exists := v["name"]; exists {
				thisChange.UpgradeInfo.ChartName = name
			}
			if version, exists := v["version"]; exists {
				thisChange.UpgradeInfo.CurrentVersion = version
				thisChange.CurrentVersionLine = lines[idx2]
				thisChange.CurrentVersionLineNumber = idx2
			}
		}
		if repoStr, exists := keys["repository"]; exists {
			thisChange.UpgradeInfo.Repository = repoStr
		}
		if name, exists := keys["name"]; exists {
			thisChange.UpgradeInfo.ChartName = name
		}
		if version, exists := keys["versionConstraint"]; exists {
			thisChange.UpgradeInfo.VersionConstraint = version
		}
		if version, exists := keys["currentVersion"]; exists {
			thisChange.UpgradeInfo.CurrentVersion = version
		}
		if autoMergeStr, exists := keys["autoMerge"]; exists {
			parsed, err := strconv.ParseBool(autoMergeStr)
			if err != nil {
				return nil, fmt.Errorf("invalid flag %s: %w", "autoMerge", err)
			}
			thisChange.UpgradeInfo.AutoMerge = &parsed
		}
		if autoAcceptStr, exists := keys["autoAccept"]; exists {
			parsed, err := strconv.ParseBool(autoAcceptStr)
			if err != nil {
				return nil, fmt.Errorf("invalid flag %s: %w", "autoAccept", err)
			}
			thisChange.UpgradeInfo.AutoApprove = &parsed
		}

		if _, err := semver.NewConstraint(thisChange.UpgradeInfo.VersionConstraint); err != nil {
			return nil, fmt.Errorf("invalid version constraint %s: %w", thisChange.UpgradeInfo.VersionConstraint, err)
		}
		if _, err := semver.NewVersion(thisChange.UpgradeInfo.CurrentVersion); err != nil {
			return nil, fmt.Errorf("invalid version %s: %w", thisChange.UpgradeInfo.CurrentVersion, err)
		}

		if !thisChange.isValid() {
			continue
		}
		ret = append(ret, &thisChange)
	}
	return ret, nil
}
