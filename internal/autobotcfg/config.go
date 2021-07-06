package autobotcfg

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"regexp"
)

type AutobotConfig struct {
	PRCreator    GithubAppConfig
	PRReviewer   *GithubAppConfig
	ChangeMakers []ChangeMakerConfig
}

type GithubAppConfig struct {
	AppID          int64
	InstallationID int64
	PEMKeyLoc      string
}

type ChangeMakerConfig struct {
	Name           string
	FileMatchRegex []string
	AutoApprove    bool
	regexp         []*regexp.Regexp
}

func (c *ChangeMakerConfig) Regex() []*regexp.Regexp {
	return c.regexp
}

func (c *ChangeMakerConfig) MatcheFile(name string) bool {
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
