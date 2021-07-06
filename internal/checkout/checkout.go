package checkout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v29/github"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

type Checkout struct {
	RepoConfig autobotcfg.RepoConfig
	Auth       githttp.AuthMethod
	Repo       *git.Repository
}

func NewCheckout(ctx context.Context, logger *zapctx.Logger, cfg autobotcfg.RepoConfig, cloneDataDirectory string, transport *ghinstallation.Transport) (*Checkout, error) {
	ch := Checkout{
		RepoConfig: cfg,
		Auth: &ghapp.DynamicHttpAuthMethod{
			Logger: logger,
			Itr:    transport,
		},
	}
	into, err := ioutil.TempDir(cloneDataDirectory, "checkout")
	if err != nil {
		return nil, fmt.Errorf("unable to clone into %s: %w", cloneDataDirectory, err)
	}
	var progress bytes.Buffer
	repo, err := git.PlainCloneContext(ctx, into, false, &git.CloneOptions{
		URL:           cfg.Location,
		Auth:          ch.Auth,
		Progress:      &progress,
		SingleBranch:  true,
		ReferenceName: plumbing.NewRemoteReferenceName("origin", cfg.Branch),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to do initial clone of %s: %w", cfg.Location, err)
	}
	ch.Repo = repo
	return &ch, nil
}

func (c *Checkout) Refresh(ctx context.Context) error {
	if err := c.Repo.FetchContext(ctx, &git.FetchOptions{
		Auth: c.Auth,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("unable to fetch update: %w", err)
	}
	return nil
}

func (c *Checkout) Clean(ctx context.Context) error {
	w, err := c.Repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to get working tree: %w", err)
	}
	if err := w.Reset(&git.ResetOptions{
		Mode: git.HardReset,
	}); err != nil {
		return fmt.Errorf("unable to reset working tree: %w", err)
	}
	if err := w.Clean(&git.CleanOptions{
		Dir: true,
	}); err != nil {
		return fmt.Errorf("unable to clean working tree: %w", err)
	}
	bItr, err := c.Repo.Branches()
	if err != nil {
		return fmt.Errorf("unable to get branch iterator: %w", err)
	}
	if err := bItr.ForEach(func(reference *plumbing.Reference) error {
		if err := c.Repo.DeleteBranch(reference.Name().String()); err != nil {
			return fmt.Errorf("unable to delete local branch %s: %w", reference.Name().String(), err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("unable to clean all branches: %w", err)
	}
	refName := plumbing.NewRemoteReferenceName("origin", c.RepoConfig.Branch)
	ref, err := c.Repo.Reference(refName, true)
	if err != nil {
		return fmt.Errorf("unable to find remote reference: %w", err)
	}
	if err := w.Checkout(&git.CheckoutOptions{
		Hash:   ref.Hash(),
		Branch: plumbing.NewBranchReferenceName("setup"),
		Create: true,
	}); err != nil {
		return fmt.Errorf("unable to check out new branch: %w", err)
	}
	return nil
}

const perRepoConfigFilename = ".gitops-autobot"

func (c *Checkout) CurrentConfig() (*autobotcfg.AutobotPerRepoConfig, error) {
	w, err := c.Repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("unable to get working tree: %w", err)
	}
	_, err = w.Filesystem.Stat(perRepoConfigFilename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("unable to stat config file: %w", err)
	}
	f, err := w.Filesystem.Open(perRepoConfigFilename)
	if err != nil {
		return nil, fmt.Errorf("unable to load per repo config: %w", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(f, &buf); err != nil {
		return nil, fmt.Errorf("unable to read data from config file: %w", err)
	}
	cfg, err := autobotcfg.LoadPerRepoConfig(&buf)
	if err != nil {
		return nil, fmt.Errorf("unable to load repo config: %w", err)
	}
	return cfg, nil
}

func (c *Checkout) SetupForWorkingTreeChanger() (*git.Worktree, *object.Commit, error) {
	w, err := c.Repo.Worktree()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get working tree: %w", err)
	}
	refName := plumbing.NewRemoteReferenceName("origin", c.RepoConfig.Branch)
	ref, err := c.Repo.Reference(refName, true)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get remote reference: %w", err)
	}
	commitObj, err := c.Repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get commit object %s: %w", ref.Hash(), err)
	}
	return w, commitObj, nil
}

func (c *Checkout) githubOwnerRepo() (string, string, error) {
	loc := c.RepoConfig.Location
	//https://github.com/cep21/circuit.git
	loc = strings.TrimPrefix(loc, "https://github.com/")
	loc = strings.TrimSuffix(loc, ".git")
	parts := strings.Split(loc, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unable to parse remote %s", c.RepoConfig.Location)
	}
	return parts[0], parts[1], nil
}

func (c *Checkout) PushAllNewBranches(ctx context.Context, client *github.Client) error {
	bItr, err := c.Repo.Branches()
	if err != nil {
		return fmt.Errorf("unable to get local branches: %w", err)
	}
	var branchesToPush []config.RefSpec
	if err := bItr.ForEach(func(reference *plumbing.Reference) error {
		if reference.Name().IsRemote() {
			return nil
		}
		if reference.Name().Short() == "setup" {
			return nil
		}
		// Push this branch and make a PR out of it
		branchesToPush = append(branchesToPush, config.RefSpec(reference.Name()+":"+reference.Name()))
		return nil
	}); err != nil {
		return fmt.Errorf("unable to iterate branches")
	}
	owner, repo, err := c.githubOwnerRepo()
	if err != nil {
		return fmt.Errorf("unable to parse repo: %w", err)
	}
	for _, b := range branchesToPush {
		err = c.Repo.PushContext(ctx, &git.PushOptions{
			RefSpecs: []config.RefSpec{
				b,
			},
			Auth: c.Auth,
		})
		if err != nil {
			return fmt.Errorf("unable to push to remote branch %s: %w", b, err)
		}
		pr, resp, err := client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
			Head: github.String(b.Reverse().Src()),
			Base: &c.RepoConfig.Branch,
		})
		if err != nil {
			return fmt.Errorf("unable to create PR for new push: %w", err)
		}
		_, _ = pr, resp
	}
	return nil
}
