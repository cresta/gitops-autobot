package prreviewer

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/google/go-github/v29/github"
	"go.uber.org/zap"
	"strings"
	"time"
)

type PrReviewer struct {
	Client        *github.Client
	Logger        *zapctx.Logger
	AutobotConfig *autobotcfg.AutobotConfig
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

func (p *PrReviewer) processPr(ctx context.Context, pr *github.PullRequest, cfg *autobotcfg.AutobotPerRepoConfig) error {
	if pr == nil {
		return nil
	}
	logger := p.Logger.With(zap.Int("pr", pr.GetNumber()))
	logger.Debug(ctx, "processing pr", zap.Any("pr", pr))
	// Will accept a PR if all the following are true
	//   * "gitops-autobot: auto-approve=true" contained in body on line by itself (spaces trimmed)
	//   * Not a draft
	//   * Enough time since creation has passed
	//   * All checks have passed
	//   * Author is allowed for auto approve
	//     * PR creator author is always allowed
	//     * Users are allowed if autobot allows user auto approve for this repository
	if !p.prAskingForAutoApproval(pr.GetBody()) {
		logger.Debug(ctx, "pr not asking for review")
		return nil
	}
	if !cfg.AllowUsersToTriggerAccept {
		if !p.AutobotConfig.PRCreator.MatchesLogin(pr.GetUser().GetLogin()) {
			logger.Debug(ctx, "not allowing users to auto accept")
			return nil
		}
	}
	if pr.Draft != nil && *pr.Draft {
		logger.Debug(ctx, "ignoring draft PR")
		return nil
	}
	if time.Since(pr.GetUpdatedAt()) < p.AutobotConfig.DelayForAutoApproval {
		logger.Debug(ctx, "ignoring pr too recently made", zap.Duration("time_left", (p.AutobotConfig.DelayForAutoApproval-time.Since(pr.GetUpdatedAt())).Round(time.Second)))
		return nil
	}
	if pr.GetHead().GetRepo().GetOwner().GetLogin() == "" {
		return fmt.Errorf("uanble to find owner's name of pr")
	}
	if pr.GetHead().GetRepo().GetName() == "" {
		return fmt.Errorf("unable to get the pr's repo")
	}
	if pr.GetNumber() == 0 {
		return fmt.Errorf("no pr number set")
	}
	owner, repo := pr.GetHead().GetRepo().GetOwner().GetLogin(), pr.GetHead().GetRepo().GetName()
	if result, err := ghapp.AllChecksPass(ctx, pr, logger, p.Client); err != nil {
		return fmt.Errorf("unable to verify checks passed: %w", err)
	} else if !result {
		return nil
	}

	reviews, _, err := p.Client.PullRequests.ListReviews(ctx, owner, repo, pr.GetNumber(), &github.ListOptions{
		Page:    0,
		PerPage: 100,
	})
	if err != nil {
		return fmt.Errorf("unable to list reviews: %w", err)
	}
	logger.Info(ctx, "reviewers", zap.Any("reviewers", reviews))
	// Must not already have a review for this commit
	for _, review := range reviews {
		if review.GetCommitID() == pr.GetHead().GetSHA() && p.AutobotConfig.PRReviewer.MatchesLogin(review.GetUser().GetLogin()) {
			logger.Debug(ctx, "already reviewed!")
			return nil
		}
	}
	_, _, err = p.Client.PullRequests.CreateReview(ctx, owner, repo, pr.GetNumber(), &github.PullRequestReviewRequest{
		Body:     github.String("auto accepted by gitops reviewbot"),
		Event:    github.String("APPROVE"),
		CommitID: pr.GetHead().SHA,
	})
	if err != nil {
		return fmt.Errorf("unable to create a review: %w", err)
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
