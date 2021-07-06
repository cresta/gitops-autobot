package autobotcfg

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"os"
	"regexp"
)

type AutobotPerRepoConfig struct {
	ChangeMakers []PerRepoChangeMakerConfig
}

type AutobotConfig struct {
	PRCreator           GithubAppConfig
	PRReviewer          *GithubAppConfig
	ChangeMakers        []ChangeMakerConfig
	CloneDataDir        string
	Repos               []RepoConfig
	DefaultRemoteBranch string
}

type GithubAppConfig struct {
	AppID          int64
	InstallationID int64
	PEMKeyLoc      string
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
	Location string
	Branch   string
}

type PerRepoChangeMakerConfig struct {
	Name           string
	FileMatchRegex []string
	AutoApprove    bool
	regexp         []*regexp.Regexp
}

type ChangeMakerConfig struct {
	Name string
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
