package helm

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/cresta/gitops-autobot/internal/cache"
	"github.com/cresta/zapctx"
	"github.com/go-logfmt/logfmt"
	"github.com/goccy/go-yaml"
	"helm.sh/helm/v3/pkg/repo"
)

type UpgradeInfo struct {
	Repository        string
	ChartName         string
	CurrentVersion    string
	VersionConstraint string
	AutoMerge         *bool
	AutoApprove       *bool
}

type LineHelmChange struct {
	UpgradeInfo              UpgradeInfo
	CurrentVersionLine       string
	CurrentVersionLineNumber int
}

func (c *LineHelmChange) isValid() bool {
	_, err1 := semver.NewConstraint(c.UpgradeInfo.VersionConstraint)
	_, err2 := semver.NewVersion(c.UpgradeInfo.CurrentVersion)
	return err1 == nil && err2 == nil && c.CurrentVersionLine != "" && c.CurrentVersionLineNumber != 0 && c.UpgradeInfo.CurrentVersion != "" &&
		c.UpgradeInfo.VersionConstraint != "" && c.UpgradeInfo.ChartName != "" && c.UpgradeInfo.Repository != ""
}

const autobotPrefix = "# gitops-autobot:"

func GroupChangesByRepo(changes []*LineHelmChange) map[string][]*LineHelmChange {
	ret := make(map[string][]*LineHelmChange)
	for _, change := range changes {
		ret[change.UpgradeInfo.Repository] = append(ret[change.UpgradeInfo.Repository], change)
	}
	return ret
}

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

type RepoInfoLoader struct {
	Client          *http.Client
	Logger          *zapctx.Logger
	LoadersByScheme map[string]IndexLoader
	Cache           cache.Cache
}

type IndexLoader interface {
	LoadIndexFile(ctx context.Context, repo string) (*repo.IndexFile, error)
}

func (r *RepoInfoLoader) LoadIndexFile(ctx context.Context, repoURL string) (*repo.IndexFile, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse repo url %s: %w", repoURL, err)
	}
	loader, exists := r.LoadersByScheme[u.Scheme]
	if !exists {
		return nil, fmt.Errorf("unable to load index file for URL (unknown scheme): %s", repoURL)
	}
	var ret repo.IndexFile
	if err := r.Cache.GetOrSet(ctx, []byte("helm_index:"+repoURL), time.Minute*5, &ret, func(ctx context.Context) (interface{}, error) {
		return loader.LoadIndexFile(ctx, repoURL)
	}); err != nil {
		return nil, fmt.Errorf("unable to load or get from cache: %w", err)
	}
	return &ret, nil
}

func yamlIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " -"))
}

type HttpLoader struct {
	Logger *zapctx.Logger
	Client *http.Client
}

func (h *HttpLoader) LoadIndexFile(ctx context.Context, url string) (*repo.IndexFile, error) {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, "/index.yaml")
	url = url + "/index.yaml"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to construct request object: %w", err)
	}
	req = req.WithContext(ctx)
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch URL %s: %w", url, err)
	}
	defer func() {
		h.Logger.IfErr(resp.Body.Close()).Warn(ctx, "unable to close http response body")
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non 200 status code: %d", resp.StatusCode)
	}
	ret, err := LoadFromReader(ctx, resp.Body, h.Logger)
	if err != nil {
		return nil, fmt.Errorf("unable to load repo from body: %w", err)
	}
	return ret, nil
}

var _ IndexLoader = &HttpLoader{}

func LoadFromReader(ctx context.Context, reader io.Reader, logger *zapctx.Logger) (*repo.IndexFile, error) {
	f, err := ioutil.TempFile("", "helm_load_from_reader")
	defer func() {
		closeErr := f.Close()
		if closeErr != nil {
			logger.IfErr(closeErr).Warn(ctx, "unable to clean up and close temp file")
			return
		}
		if err := os.Remove(f.Name()); err != nil {
			logger.IfErr(err).Warn(ctx, "unable to remove temp file")
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

type ChangeParser struct {
	Logger *zapctx.Logger
}

type VersionChange struct {
	PreviousLine string
	NewLine      string
	NewVersion   string
	LineNumber   int
}

func (c *ChangeParser) LoadVersions(_ context.Context, change *LineHelmChange, index *repo.IndexFile) (*VersionChange, error) {
	constraint, err := semver.NewConstraint(change.UpgradeInfo.VersionConstraint)
	if err != nil {
		return nil, fmt.Errorf("unable to parse constraint %s: %w", change.UpgradeInfo.VersionConstraint, err)
	}
	allVersions := index.Entries[change.UpgradeInfo.ChartName]
	if len(allVersions) == 0 {
		return nil, nil
	}
	matchingVersions := make([]string, 0)
	matchingVersions = append(matchingVersions, change.UpgradeInfo.CurrentVersion)
	currentVersion, err := semver.NewVersion(change.UpgradeInfo.CurrentVersion)
	if err != nil {
		return nil, fmt.Errorf("uanble to parse current version: %w", err)
	}
	highestVersion := currentVersion
	for _, v := range allVersions {
		thisVersion, err := semver.NewVersion(v.Version)
		if err != nil {
			return nil, fmt.Errorf("uanble to parse next version: %w", err)
		}
		if !constraint.Check(thisVersion) {
			continue
		}
		if thisVersion.GreaterThan(highestVersion) {
			highestVersion = thisVersion
		}
	}
	if highestVersion == currentVersion {
		return nil, nil
	}
	parts := strings.SplitN(change.CurrentVersionLine, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid current line %s", change.CurrentVersionLine)
	}
	return &VersionChange{
		PreviousLine: change.CurrentVersionLine,
		NewLine:      parts[0] + ": " + highestVersion.String(),
		LineNumber:   change.CurrentVersionLineNumber,
		NewVersion:   highestVersion.String(),
	}, nil
}
