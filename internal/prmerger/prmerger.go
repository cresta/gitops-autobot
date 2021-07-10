package prmerger

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/google/go-github/v29/github"
	"go.uber.org/zap"
	"strings"
)

type PRMerger struct {
	Client        *github.Client
	Logger        *zapctx.Logger
	AutobotConfig *autobotcfg.AutobotConfig
}

func (p *PRMerger) Execute(ctx context.Context) error {
	for _, r := range p.AutobotConfig.Repos {
		repoCfg, err := ghapp.FetchMasterConfigFile(ctx, p.Client, r)
		if err != nil {
			return fmt.Errorf("uanble to fetch repo content for %s: %w", r, err)
		}
		if !repoCfg.AllowAutoMerge {
			p.Logger.Debug(ctx, "not allowed to auto merge")
			continue
		}
		prs, err := ghapp.EveryPullRequest(ctx, p.Client, r)
		if err != nil {
			return fmt.Errorf("cannot list eveyr pr: %w", err)
		}
		for _, pr := range prs {
			if err := p.processPr(ctx, pr, repoCfg); err != nil {
				return fmt.Errorf("unable to process pr: %w", err)
			}
		}
	}
	return nil
}

func (p *PRMerger) processPr(ctx context.Context, pr *github.PullRequest, cfg *autobotcfg.AutobotPerRepoConfig) error {
	if pr == nil {
		return nil
	}
	// Will merge a PR if all these are true
	//   * "gitops-autobot: auto-merge=true" contained in body on line by itself (spaces trimmed)
	//   * Not a draft
	//   * All checks have passed
	//   * PR is mergable
	logger := p.Logger.With(zap.Int("pr", pr.GetNumber()))
	logger.Debug(ctx, "processing pr", zap.Any("pr", pr))
	if pr.GetMerged() {
		logger.Debug(ctx, "already merged!")
		return nil
	}
	if pr.GetMergeableState() != "clean" {
		logger.Info(ctx, "cannot merge with state not clean", zap.String("state", pr.GetMergeableState()))
	}
	if !p.prAskingForAutoMerge(pr.GetBody()) {
		logger.Debug(ctx, "pr not asking for review")
		return nil
	}
	if pr.Draft != nil && *pr.Draft {
		logger.Debug(ctx, "ignoring draft PR")
		return nil
	}
	owner, repo := pr.GetHead().GetRepo().GetOwner().GetLogin(), pr.GetHead().GetRepo().GetName()
	if result, err := ghapp.AllChecksPass(ctx, pr, logger, p.Client); err != nil {
		return fmt.Errorf("unable to verify checks passed: %w", err)
	} else if !result {
		return nil
	}
	_, _, err := p.Client.PullRequests.Merge(ctx, owner, repo, pr.GetNumber(), "", &github.PullRequestOptions{
		CommitTitle: "",
		SHA:         pr.GetHead().GetSHA(),
		MergeMethod: "squash",
	})
	if err != nil {
		return fmt.Errorf("unable to merge PR: %w", err)
	}
	return nil
}

func (p *PRMerger) prAskingForAutoMerge(msg string) bool {
	for _, line := range strings.Split(msg, "\n") {
		if strings.TrimSpace(line) == "gitops-autobot: auto-merge=true" {
			return true
		}
	}
	return false
}
