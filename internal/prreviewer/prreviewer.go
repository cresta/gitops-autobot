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
	Client          *ghapp.GithubAPI
	Logger          *zapctx.Logger
	AutobotConfig   *autobotcfg.AutobotConfig
	currentReviewer *ghapp.UserInfo
}

func (p *PrReviewer) Execute(ctx context.Context) error {
	if err := p.populateSelf(ctx); err != nil {
		return fmt.Errorf("unable to populate self information: %w", err)
	}
	for _, r := range p.AutobotConfig.Repos {
		repoCfg, err := p.Client.FetchMasterConfigFile(ctx, r)
		if err != nil {
			return fmt.Errorf("uanble to fetch repo content for %s: %w", r, err)
		}
		if !repoCfg.AllowAutoReview {
			p.Logger.Debug(ctx, "not allowed to auto review")
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

func (p *PrReviewer) populateSelf(ctx context.Context) error {
	if p.currentReviewer != nil {
		return nil
	}
	ret, err := p.Client.Self(ctx)
	if err != nil {
		return fmt.Errorf("unable to find self: % w", err)
	}
	p.currentReviewer = ret
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
	if !cfg.AllowUsersToTriggerAccept {
		//if !p.AutobotConfig.PRCreator.MatchesLogin(pr.GetUser().GetLogin()) {
		//	logger.Debug(ctx, "not allowing users to auto accept")
		//	return nil
		//}
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

	for _, r := range pr.Reviews.Nodes {
		if r.State != "APPROVED" {
			logger.Debug(ctx, "ignore not approved review")
		}
		if r.Commit.Oid != pr.HeadRef.Target.Oid {
			logger.Debug(ctx, "ignore sha mismatch")
		}
		if r.Author.Bot.Id == p.currentReviewer.Id || r.Author.User.Id == p.currentReviewer.Id {
			logger.Debug(ctx, "skip because already reviewed")
			return nil
		}
	}

	event := githubv4.PullRequestReviewEventApprove
	body := githubv4.String("auto accepted by gitops reviewbot")
	if _, err := p.Client.AcceptPullRequest(ctx, githubv4.AddPullRequestReviewInput{
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
