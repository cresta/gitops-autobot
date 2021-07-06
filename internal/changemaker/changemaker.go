package changemaker

import (
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Commiter interface {
}

type gitCommitter struct {
	DefaultCommitOptions *git.CommitOptions
}

var _ GitCommitter = &gitCommitter{}

func (g *gitCommitter) Commit(w *git.Worktree, msg string, opts *git.CommitOptions) (plumbing.Hash, error) {
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
	return w.Commit(msg, &co)
}

type GitCommitter interface {
	Commit(w *git.Worktree, msg string, opts *git.CommitOptions) (plumbing.Hash, error)
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

func (f *Factory) Load(ChangeMakers []autobotcfg.ChangeMakerConfig, repoCfg autobotcfg.AutobotPerRepoConfig) ([]WorkingTreeChanger, error) {
	var ret []WorkingTreeChanger
	for _, rcm := range repoCfg.ChangeMakers {
		for _, cm := range ChangeMakers {
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
