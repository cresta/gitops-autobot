package checkout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v29/github"
	"github.com/shurcooL/githubv4"
	"go.uber.org/zap"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

type Checkout struct {
	RepoConfig autobotcfg.RepoConfig
	auth       transport.AuthMethod
	Repo       *git.Repository
	Logger     *zapctx.Logger
}

func NewCheckout(ctx context.Context, logger *zapctx.Logger, cfg autobotcfg.RepoConfig, cloneDataDirectory string, auth transport.AuthMethod) (*Checkout, error) {
	ch := Checkout{
		RepoConfig: cfg,
		auth:       auth,
		Logger:     logger,
	}
	into, err := ioutil.TempDir(cloneDataDirectory, "checkout")
	if err != nil {
		return nil, fmt.Errorf("unable to clone into %s: %w", cloneDataDirectory, err)
	}
	var progress bytes.Buffer
	repo, err := git.PlainCloneContext(ctx, into, false, &git.CloneOptions{
		URL:           cfg.CloneURL(),
		Auth:          ch.auth,
		Progress:      &progress,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName(cfg.Branch),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to do initial clone of %s: %w", cfg.CloneURL(), err)
	}
	ch.Repo = repo
	return &ch, nil
}

func (c *Checkout) Refresh(ctx context.Context) error {
	c.Logger.Debug(ctx, "+Checkout.Refresh")
	defer c.Logger.Debug(ctx, "-Checkout.Refresh")
	remotes, err := c.Repo.Remotes()
	if err != nil {
		return fmt.Errorf("unable to list remotes: %w", err)
	}
	for _, r := range remotes {
		if err := r.FetchContext(ctx, &git.FetchOptions{
			Auth: c.auth,
		}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("unable to fetch from remote %s: %w", r.Config().Name, err)
		}
	}
	return nil
}

const gitopsAutobotDefaultBranch = "gitops-autobot-start"

func (c *Checkout) Clean(ctx context.Context) error {
	c.Logger.Debug(ctx, "+Checkout.Clean")
	defer c.Logger.Debug(ctx, "-Checkout.Clean")
	c.Logger.Debug(ctx, "cleaning")
	w, base, err := c.SetupForWorkingTreeChanger(ctx)
	if err != nil {
		return fmt.Errorf("unable to setup for cleaning: %w", err)
	}
	if err := c.Repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(gitopsAutobotDefaultBranch), base.Hash)); err != nil {
		return fmt.Errorf("unable to set new branch ref: %w", err)
	}
	if err := w.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: base.Hash,
	}); err != nil {
		return fmt.Errorf("unable to reset working tree: %w", err)
	}
	if err := w.Clean(&git.CleanOptions{
		Dir: true,
	}); err != nil {
		return fmt.Errorf("unable to clean working tree: %w", err)
	}
	a, b := c.Repo.Config()
	_, _ = a, b
	if err := w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(gitopsAutobotDefaultBranch),
	}); err != nil && !errors.Is(err, git.ErrBranchExists) {
		return fmt.Errorf("unable to check out new branch: %w", err)
	}
	if err := w.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: base.Hash,
	}); err != nil {
		return fmt.Errorf("unable to reset working tree: %w", err)
	}
	bItr, err := c.Repo.Branches()
	if err != nil {
		return fmt.Errorf("unable to get branch iterator: %w", err)
	}
	defer bItr.Close()
	if err := bItr.ForEach(func(reference *plumbing.Reference) error {
		if reference.Name().Short() == gitopsAutobotDefaultBranch {
			return nil
		}
		c.Logger.Debug(ctx, "deleting a branch", zap.String("branch", reference.String()))
		if err := c.Repo.Storer.RemoveReference(reference.Name()); err != nil {
			return fmt.Errorf("unable to remove ref %s: %w", reference.Name().String(), err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("unable to iterate branches")
	}
	return nil
}

const perRepoConfigFilename = ".gitops-autobot"

func (c *Checkout) CurrentConfig(ctx context.Context) (*autobotcfg.AutobotPerRepoConfig, error) {
	c.Logger.Debug(ctx, "+Checkout.CurrentConfig")
	defer c.Logger.Debug(ctx, "-Checkout.CurrentConfig")
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
	defer func() {
		c.Logger.IfErr(f.Close()).Warn(context.Background(), "unable to close opened config file")
	}()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, f); err != nil {
		return nil, fmt.Errorf("unable to read data from config file: %w", err)
	}
	c.Logger.Debug(context.Background(), "repo config", zap.String("cfg", buf.String()))
	cfg, err := autobotcfg.LoadPerRepoConfig(&buf)
	if err != nil {
		return nil, fmt.Errorf("unable to load repo config: %w", err)
	}
	return cfg, nil
}

func (c *Checkout) SetupForWorkingTreeChanger(ctx context.Context) (*git.Worktree, *object.Commit, error) {
	c.Logger.Debug(ctx, "+Checkout.SetupForWorkingTreeChanger")
	defer c.Logger.Debug(ctx, "-Checkout.SetupForWorkingTreeChanger")
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

func (c *Checkout) PushAllNewBranches(ctx context.Context, client ghapp.GithubAPI) error {
	c.Logger.Debug(ctx, "+Checkout.PushAllNewBranches")
	defer c.Logger.Debug(ctx, "-Checkout.PushAllNewBranches")
	var branchesToPush []config.RefSpec
	bItr, err := c.Repo.Branches()
	if err != nil {
		return fmt.Errorf("unable to get branch iterator: %w", err)
	}
	defer bItr.Close()
	toPushToPr := make(map[config.RefSpec]*github.NewPullRequest)
	if err := bItr.ForEach(func(reference *plumbing.Reference) error {
		if reference.Name().Short() == gitopsAutobotDefaultBranch {
			return nil
		}
		commitObj, err := c.Repo.CommitObject(reference.Hash())
		if err != nil {
			return fmt.Errorf("unable to find commit object for branch %s: %w", reference.Name().String(), err)
		}
		refSpec := config.RefSpec(reference.Name().String() + ":" + reference.Name().String())
		toPushToPr[refSpec] = extractGithubTitleAndMsg(commitObj.Message, reference.Name().Short())
		c.Logger.Debug(ctx, "pushing a branch", zap.String("branch", reference.String()))
		branchesToPush = append(branchesToPush, refSpec)
		return nil
	}); err != nil {
		return fmt.Errorf("unable to iterate branches")
	}
	c.Logger.Debug(ctx, "pushing new branches", zap.Any("all_branches", branchesToPush))
	repoInfo, queryErr := client.RepositoryInfo(ctx, c.RepoConfig.Owner, c.RepoConfig.Name)
	if queryErr != nil {
		return fmt.Errorf("unable to execute graphql query: %w", err)
	}
	for _, b := range branchesToPush {
		// Fetch commit object to build github PR message
		err = c.Repo.PushContext(ctx, &git.PushOptions{
			RemoteName: "origin",
			RefSpecs: []config.RefSpec{
				b,
			},
			Auth: c.auth,
		})
		if err != nil {
			if strings.HasPrefix(err.Error(), "non-fast-forward update") {
				c.Logger.Debug(ctx, "non fast forward update for branch and assumed it is already in PR", zap.String("branch", b.String()))
				continue
			}
			return fmt.Errorf("unable to push to remote branch %s: %w", b, err)
		}
		prObj := toPushToPr[b]
		if prObj == nil {
			prObj = &github.NewPullRequest{}
		}
		prObj.Base = &c.RepoConfig.Branch
		prObj.Head = github.String(b.Reverse().Src())
		if _, err := client.CreatePullRequest(ctx, c.RepoConfig.Owner, c.RepoConfig.Name, githubv4.CreatePullRequestInput{
			RepositoryID: repoInfo.Repository.Id,
			BaseRefName:  "main",
			HeadRefName:  githubv4.String(b.Src()),
			Title:        githubv4.String(*prObj.Title),
			Body:         githubv4.NewString(githubv4.String(*prObj.Body)),
		}); err != nil {
			return fmt.Errorf("unable to create PR for new push: %w", err)
		}
	}
	return nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[0:maxLen]
}

func extractGithubTitleAndMsg(message string, branchName string) *github.NewPullRequest {
	parts := strings.SplitN(message, "\n", 2)
	if len(parts) == 0 {
		return &github.NewPullRequest{
			Title: github.String("Commit to branch " + branchName),
		}
	}
	if len(parts) == 1 {
		return &github.NewPullRequest{
			Title: github.String(truncateString(strings.TrimSpace(message), 75)),
		}
	}
	return &github.NewPullRequest{
		Title: github.String(truncateString(strings.TrimSpace(parts[0]), 75)),
		Body:  github.String(strings.TrimSpace(parts[1])),
	}
}
