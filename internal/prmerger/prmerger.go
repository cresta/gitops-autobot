package prmerger

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/shurcooL/githubv4"
	"go.uber.org/zap"
	"strings"
)

type PRMerger struct {
	Client        *ghapp.GithubAPI
	Logger        *zapctx.Logger
	AutobotConfig *autobotcfg.AutobotConfig
}

func (p *PRMerger) Execute(ctx context.Context) error {
	for _, r := range p.AutobotConfig.Repos {
		repoCfg, err := p.Client.FetchMasterConfigFile(ctx, r)
		if err != nil {
			return fmt.Errorf("uanble to fetch repo content for %s: %w", r, err)
		}
		if !repoCfg.AllowAutoMerge {
			p.Logger.Debug(ctx, "not allowed to auto merge")
			continue
		}
		prs, err := p.Client.EveryPullRequest(ctx, r)
		if err != nil {
			return fmt.Errorf("cannot list every pr: %w", err)
		}
		for _, pr := range prs.Repository.PullRequests.Nodes {
			if err := p.processPr(ctx, pr, repoCfg); err != nil {
				return fmt.Errorf("unable to process pr: %w", err)
			}
		}
	}
	return nil
}

func (p *PRMerger) processPr(ctx context.Context, pr ghapp.GraphQLPRQueryNode, cfg *autobotcfg.AutobotPerRepoConfig) error {
	// Will merge a PR if all these are true
	//   * "gitops-autobot: auto-merge=true" contained in body on line by itself (spaces trimmed)
	//   * Not a draft
	//   * All checks have passed
	//   * PR is mergable
	logger := p.Logger.With(zap.Int32("pr", int32(pr.Number)))
	logger.Debug(ctx, "processing pr", zap.Any("pr", pr))
	if pr.Merged {
		logger.Debug(ctx, "already merged!")
		return nil
	}
	if pr.Mergeable != githubv4.MergeableStateMergeable {
		logger.Info(ctx, "cannot merge with state not clean", zap.String("state", string(pr.Mergeable)))
	}
	if !p.prAskingForAutoMerge(string(pr.Body)) {
		logger.Debug(ctx, "pr not asking for review")
		return nil
	}
	if pr.IsDraft {
		logger.Debug(ctx, "ignoring draft PR")
		return nil
	}
	if pr.HeadRef.Target.Commit.StatusCheckRollup.State != githubv4.StatusStateSuccess {
		logger.Debug(ctx, "status state not success", zap.String("state", string(pr.HeadRef.Target.Commit.StatusCheckRollup.State)))
		return nil
	}

	method := githubv4.PullRequestMergeMethodSquash
	if _, err := p.Client.MergePullRequest(ctx, githubv4.MergePullRequestInput{
		PullRequestID:   pr.Id,
		ExpectedHeadOid: &pr.HeadRef.Target.Oid,
		MergeMethod:     &method,
	}); err != nil {
		return fmt.Errorf("unable to do create a merge: %w", err)
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
