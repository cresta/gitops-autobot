package changemaker

import (
	"bytes"
	"fmt"
	"time"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v2"
)

type gitCommitter struct {
	DefaultCommitOptions *git.CommitOptions
}

func CommitterFromConfig(config autobotcfg.CommitterConfig) (GitCommitter, error) {
	return &gitCommitter{DefaultCommitOptions: &git.CommitOptions{
		Author: &object.Signature{
			Name:  config.AuthorName,
			Email: config.AuthorEmail,
		},
	}}, nil
}

var _ GitCommitter = &gitCommitter{}

func (g *gitCommitter) Commit(w *git.Worktree, msg string, opts *git.CommitOptions, _ autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) (plumbing.Hash, error) {
	now := time.Now()
	var co git.CommitOptions
	if g.DefaultCommitOptions != nil {
		co = *g.DefaultCommitOptions
	}
	if opts != nil {
		co.All = opts.All
		if opts.Committer != nil {
			co.Committer = opts.Committer
		}
		if opts.SignKey != nil {
			co.SignKey = opts.SignKey
		}
		if opts.Author != nil {
			co.Author = opts.Author
		}
		if opts.Parents != nil {
			co.Parents = opts.Parents
		}
	}
	if co.Author != nil && co.Author.When.IsZero() {
		co.Author.When = now
	}
	if co.Committer != nil && co.Committer.When.IsZero() {
		co.Committer.When = now
	}
	msg = tagCommitMessage(msg, perRepo)
	return w.Commit(msg, &co)
}

type GitCommitter interface {
	Commit(w *git.Worktree, msg string, opts *git.CommitOptions, _ autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) (plumbing.Hash, error)
}

type WorkingTreeChanger interface {
	// ChangeWorkingTree should create any branches it needs.  Each branch
	// will be pushed as a separate PR.  If the branch name exists in the remote, we will attempt
	// a push, but ignore any errors around non-fast-forward.
	ChangeWorkingTree(w *git.Worktree, baseCommit *object.Commit, gitCommitter GitCommitter) error
}

type WorkingTreeChangerFactory func(cfg autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) ([]WorkingTreeChanger, error)

type Factory struct {
	Factories []WorkingTreeChangerFactory
}

func (f *Factory) Load(changeMakers []autobotcfg.ChangeMakerConfig, repoCfg autobotcfg.AutobotPerRepoConfig) ([]WorkingTreeChanger, error) {
	var ret []WorkingTreeChanger
	for _, rcm := range repoCfg.ChangeMakers {
		for _, cm := range changeMakers {
			if cm.Name != rcm.Name {
				continue
			}
			loaded := false
			for _, factory := range f.Factories {
				changers, err := factory(cm, rcm)
				if err != nil {
					return nil, fmt.Errorf("unable to load change maker for %s: %w", cm.Name, err)
				}
				if changers != nil {
					ret = append(ret, changers...)
					loaded = true
					break
				}
			}
			if !loaded {
				return nil, fmt.Errorf("unable to discover change maker for %s", cm.Name)
			}
		}
	}
	return ret, nil
}

func tagCommitMessage(msg string, cfg autobotcfg.PerRepoChangeMakerConfig) string {
	if cfg.AutoApprove {
		msg += "\ngitops-autobot: auto-approve=true\n"
	}
	if cfg.AutoMerge {
		msg += "\ngitops-autobot: auto-merge=true\n"
	}
	return msg
}

func ReEncodeYAML(pluginIn, pluginOut interface{}) error {
	if pluginIn == nil {
		return nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(pluginIn); err != nil {
		return fmt.Errorf("unable to re encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("unable to close encoder: %w", err)
	}
	dec := yaml.NewDecoder(&buf)
	dec.SetStrict(true)
	if err := dec.Decode(pluginOut); err != nil {
		return fmt.Errorf("unable to decode plugin output: %w", err)
	}
	return nil
}
