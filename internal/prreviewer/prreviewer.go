package prreviewer

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/shurcooL/githubv4"
	"go.uber.org/zap"
	"strings"
	"time"
)

type PrReviewer struct {
	Client        ghapp.GithubAPI
	Logger        *zapctx.Logger
	AutobotConfig *autobotcfg.AutobotConfig
	PRMaker       *ghapp.UserInfo
}

func (p *PrReviewer) Execute(ctx context.Context) error {
	for _, r := range p.AutobotConfig.Repos {
		repoCfg, err := ghapp.FetchMasterConfigFile(ctx, p.Client, r)
		if err != nil {
			return fmt.Errorf("uanble to fetch repo content for %s: %w", r, err)
		}
		if !repoCfg.AllowAutoReview {
			p.Logger.Debug(ctx, "not allowed to auto review")
			continue
		}
		prs, err := p.Client.EveryOpenPullRequest(ctx, r.Owner, r.Name)
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

func (p *PrReviewer) processPr(ctx context.Context, pr ghapp.GraphQLPRQueryNode, cfg *autobotcfg.AutobotPerRepoConfig) error {
	logger := p.Logger.With(zap.Int32("pr", int32(pr.Number)))
	logger.Debug(ctx, "processing pr", zap.Any("pr", pr))
	// Will accept a PR if all the following are true
	//   * "gitops-autobot: auto-approve=true" contained in body on line by itself (spaces trimmed)
	//   * Not a draft
	//   * Enough time since creation has passed
	//   * All checks have passed
	//   * Author is allowed for auto approve
	//     * PR creator author is always allowed
	//     * Users are allowed if autobot allows user auto approve for this repository
	if !p.prAskingForAutoApproval(string(pr.Body)) {
		logger.Debug(ctx, "pr not asking for review")
		return nil
	}
	if p.PRMaker.Id != pr.Author.Bot.Id && p.PRMaker.Id != pr.Author.User.Id {
		if !cfg.AllowUsersToTriggerAccept {
			if p.PRMaker == nil {
				logger.Debug(ctx, "not allowing users to accept reviews")
				return nil
			}
		}
		if pr.IsCrossRepository {
			logger.Debug(ctx, "auto approve not allowed for cross repository PRs")
			return nil
		}
	}
	if pr.IsDraft {
		logger.Debug(ctx, "ignoring draft PR")
		return nil
	}
	if time.Since(pr.UpdatedAt.Time) < p.AutobotConfig.DelayForAutoApproval {
		logger.Debug(ctx, "ignoring pr too recently made", zap.Duration("time_left", (p.AutobotConfig.DelayForAutoApproval-time.Since(pr.UpdatedAt.Time)).Round(time.Second)))
		return nil
	}
	if pr.HeadRef.Target.Commit.StatusCheckRollup.State != githubv4.StatusStateSuccess {
		logger.Debug(ctx, "status state not success", zap.String("state", string(pr.HeadRef.Target.Commit.StatusCheckRollup.State)))
		return nil
	}

	if pr.ViewerLatestReview.Commit.Oid == pr.HeadRef.Target.Oid {
		logger.Debug(ctx, "already reviewed this PR")
		return nil
	}

	event := githubv4.PullRequestReviewEventApprove
	body := githubv4.String("auto accepted by gitops reviewbot")
	if _, err := p.Client.AcceptPullRequest(ctx, string(pr.Repository.Owner.Login), string(pr.Repository.Name), githubv4.AddPullRequestReviewInput{
		PullRequestID: pr.Id,
		CommitOID:     &pr.HeadRef.Target.Oid,
		Body:          &body,
		Event:         &event,
	}); err != nil {
		return fmt.Errorf("uanble to add PR review: %w", err)
	}

	return nil
}

func (p *PrReviewer) prAskingForAutoApproval(msg string) bool {
	for _, line := range strings.Split(msg, "\n") {
		if strings.TrimSpace(line) == "gitops-autobot: auto-approve=true" {
			return true
		}
	}
	return false
}
