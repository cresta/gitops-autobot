package helm

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/versionfetch"
	"github.com/cresta/zapctx"
	"github.com/go-logfmt/logfmt"
	"github.com/goccy/go-yaml"
	"helm.sh/helm/v3/pkg/repo"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Helm struct {
}

type helmUpgradeInfo struct {
	Repository        string
	ChartName         string
	CurrentVersion    string
	VersionConstraint string
}

type LineHelmChange struct {
	upgradeInfo              helmUpgradeInfo
	currentVersionLine       string
	currentVersionLineNumber int
}

func (c *LineHelmChange) isValid() bool {
	return c.currentVersionLine != "" && c.currentVersionLineNumber != 0 && c.upgradeInfo.CurrentVersion != "" &&
		c.upgradeInfo.VersionConstraint != "" && c.upgradeInfo.ChartName != "" && c.upgradeInfo.Repository != ""
}

const autobotPrefix = "# gitops-autobot:"

func ParseHelmReleaseYAML(lines []string) ([]*LineHelmChange, error) {
	var ret []*LineHelmChange
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
				thisChange.upgradeInfo.Repository = repoStr
			}
			if name, exists := v["name"]; exists {
				thisChange.upgradeInfo.ChartName = name
			}
			if version, exists := v["version"]; exists {
				thisChange.upgradeInfo.CurrentVersion = version
				thisChange.currentVersionLine = lines[idx2]
				thisChange.currentVersionLineNumber = idx2
			}
		}
		if repoStr, exists := keys["repository"]; exists {
			thisChange.upgradeInfo.Repository = repoStr
		}
		if name, exists := keys["name"]; exists {
			thisChange.upgradeInfo.ChartName = name
		}
		if version, exists := keys["versionConstraint"]; exists {
			thisChange.upgradeInfo.VersionConstraint = version
		}
		if version, exists := keys["currentVersion"]; exists {
			thisChange.upgradeInfo.CurrentVersion = version
		}

		if !thisChange.isValid() {
			continue
		}
		ret = append(ret, &thisChange)
	}
	return ret, nil
}

type RepoInfoLoader struct {
	Client *http.Client
	Logger *zapctx.Logger
}

func (r *RepoInfoLoader) LoadIndexFile(ctx context.Context, repo string) (*repo.IndexFile, error) {
	u, err := url.Parse(repo)
	if err != nil {
		return nil, fmt.Errorf("unable to parse repo url %s: %w", repo, err)
	}

}

func yamlIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " -"))
}

func LoadFromURL(ctx context.Context, client http.Client, url string, onCleanupErr func(error)) (*repo.IndexFile, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to construct request object: %w", err)
	}
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch URL %s: %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			onCleanupErr(err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non 200 status code: %d", resp.StatusCode)
	}
	ret, err := LoadFromReader(resp.Body, onCleanupErr)
	if err != nil {
		return nil, fmt.Errorf("unable to load repo from body: %w", err)
	}
	return ret, nil
}

func LoadFromReader(reader io.Reader, onCleanupErr func(error)) (*repo.IndexFile, error) {
	f, err := ioutil.TempFile("", "helm_load_from_reader")
	defer func() {
		closeErr := f.Close()
		if closeErr != nil {
			onCleanupErr(closeErr)
			return
		}
		if err := os.Remove(f.Name()); err != nil {
			onCleanupErr(err)
		}
	}()
	if _, err := io.Copy(f, reader); err != nil {
		return nil, fmt.Errorf("unable to copy from response body: %w", err)
	}
	ret, err := repo.LoadIndexFile(f.Name())
	if err != nil {
		return nil, fmt.Errorf("unable to parse index file: %w", err)
	}
	return ret, nil
}

func (h *Helm) AllVersion(ctx context.Context, repo string, name string) {

}

var _ versionfetch.VersionFetch = &Helm{}
