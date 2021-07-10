package autobotcfg

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

type AutobotPerRepoConfig struct {
	ChangeMakers              []PerRepoChangeMakerConfig `yaml:"changeMakers"`
	AllowAutoReview           bool                       `yaml:"allowAutoReview"`
	AllowUsersToTriggerAccept bool                       `yaml:"allowUsersToTriggerAccept"`
	AllowAutoMerge            bool                       `yaml:"allowAutoMerge"`
}

type AutobotConfig struct {
	PRCreator            GithubAppConfig     `yaml:"prCreator"`
	PRReviewer           *GithubAppConfig    `yaml:"prReviewer"`
	ChangeMakers         []ChangeMakerConfig `yaml:"changeMakers"`
	CloneDataDir         string              `yaml:"cloneDataDir"`
	Repos                []RepoConfig        `yaml:"repos"`
	DefaultRemoteBranch  string              `yaml:"defaultRemoteBranch"`
	ComitterConfig       ComitterConfig      `yaml:"comitterConfig"`
	DelayForAutoApproval time.Duration       `yaml:"delayForAutoApproval"`
}

type ComitterConfig struct {
	AuthorName  string `yaml:"authorName"`
	AuthorEmail string `yaml:"authorEmail"`
}

type GithubAppConfig struct {
	AppID          int64  `yaml:"appID"`
	InstallationID int64  `yaml:"installationID"`
	PEMKeyLoc      string `yaml:"PEMKeyLoc"`
	Name           string `yaml:"name"`
}

func (g GithubAppConfig) MatchesLogin(s string) bool {
	return g.Name == s || g.Name+"[bot]" == s
}

func (g *GithubAppConfig) Validate() error {
	if g == nil {
		return nil
	}
	if _, err := os.Stat(g.PEMKeyLoc); os.IsNotExist(err) {
		return fmt.Errorf("unable to find PEM key %s", g.PEMKeyLoc)
	}
	return nil
}

type RepoConfig struct {
	Location string `yaml:"location"`
	Branch   string `yaml:"branch"`
}

func (r *RepoConfig) GithubOwnerRepo() (string, string, error) {
	loc := r.Location
	//https://github.com/cep21/circuit.git
	loc = strings.TrimPrefix(loc, "https://github.com/")
	loc = strings.TrimSuffix(loc, ".git")
	parts := strings.Split(loc, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unable to parse remote %s", r.Location)
	}
	return parts[0], parts[1], nil
}

type PerRepoChangeMakerConfig struct {
	Name           string   `yaml:"name"`
	FileMatchRegex []string `yaml:"fileMatchRegex"`
	AutoApprove    bool     `yaml:"autoApprove"`
	regexp         []*regexp.Regexp
}

type ChangeMakerConfig struct {
	Name string `yaml:"name"`
}

func (c *PerRepoChangeMakerConfig) Regex() []*regexp.Regexp {
	return c.regexp
}

func (c *PerRepoChangeMakerConfig) MatcheFile(name string) bool {
	if len(c.regexp) == 0 {
		return true
	}
	for _, r := range c.regexp {
		if r.MatchString(name) {
			return true
		}
	}
	return false
}

func Load(cfg io.WriterTo) (*AutobotConfig, error) {
	var buf bytes.Buffer
	if _, err := cfg.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("unable to read config: %w", err)
	}
	var ret AutobotConfig
	d := yaml.NewDecoder(&buf)
	d.SetStrict(true)
	if err := d.Decode(&ret); err != nil {
		return nil, fmt.Errorf("unable to decode config file: %w", err)
	}
	if ret.DefaultRemoteBranch == "" {
		ret.DefaultRemoteBranch = "master"
	}
	if ret.DelayForAutoApproval == 0 {
		ret.DelayForAutoApproval = time.Minute
	}
	if ret.CloneDataDir == "" {
		ret.CloneDataDir = os.TempDir()
	}
	for idx := range ret.Repos {
		if ret.Repos[idx].Branch == "" {
			ret.Repos[idx].Branch = ret.DefaultRemoteBranch
		}
	}
	if err := ret.PRCreator.Validate(); err != nil {
		return nil, fmt.Errorf("unable to validate pr creator: %w", err)
	}
	if err := ret.PRReviewer.Validate(); err != nil {
		return nil, fmt.Errorf("unable to validate pr reviewer: %w", err)
	}
	return &ret, nil
}

func LoadPerRepoConfig(cfg io.WriterTo) (*AutobotPerRepoConfig, error) {
	var buf bytes.Buffer
	if _, err := cfg.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("unable to read config: %w", err)
	}
	var ret AutobotPerRepoConfig
	d := yaml.NewDecoder(&buf)
	d.SetStrict(true)
	if err := d.Decode(&ret); err != nil {
		return nil, fmt.Errorf("unable to decode config file: %w", err)
	}
	for idx, cm := range ret.ChangeMakers {
		for _, fmr := range cm.FileMatchRegex {
			re, err := regexp.Compile(fmr)
			if err != nil {
				return nil, fmt.Errorf("invalid regex %s: %w", fmr, err)
			}
			cm.regexp = append(cm.regexp, re)
		}
		ret.ChangeMakers[idx] = cm
	}
	return &ret, nil
}
