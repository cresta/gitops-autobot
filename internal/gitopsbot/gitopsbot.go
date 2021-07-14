package gitopsbot

import (
	"context"
	"fmt"
	"time"

	"github.com/cresta/gitops-autobot/internal/checkout"
	"github.com/cresta/gitops-autobot/internal/prcreator"
	"github.com/cresta/gitops-autobot/internal/prmerger"
	"github.com/cresta/gitops-autobot/internal/prreviewer"
	"github.com/cresta/zapctx"
	"go.uber.org/zap"
)

type GitopsBot struct {
	PRCreator    *prcreator.PrCreator
	PrReviewer   *prreviewer.PrReviewer
	PRMerger     *prmerger.PRMerger
	Checkouts    []*checkout.Checkout
	Logger       *zapctx.Logger
	CronInterval time.Duration
	cronTrigger  chan struct{}
	stopTrigger  chan struct{}
}

func (g *GitopsBot) Execute(ctx context.Context) error {
	g.Logger.Info(ctx, "+GitopsBot.Execute")
	defer g.Logger.Info(ctx, "-GitopsBot.Execute")
	for _, c := range g.Checkouts {
		l := g.Logger.With(zap.Stringer("checkout", c.RepoConfig))
		if err := g.PRCreator.Execute(ctx, c); err != nil {
			l.IfErr(err).Warn(ctx, "unable to execute PR creation")
			return fmt.Errorf("unable to create prs for %s: %w", c.RepoConfig.String(), err)
		}
	}
	if err := g.PrReviewer.Execute(ctx); err != nil {
		return fmt.Errorf("unable to review any PRs: %w", err)
	}
	if err := g.PRMerger.Execute(ctx); err != nil {
		return fmt.Errorf("unable to execute any PRs: %w", err)
	}
	return nil
}

func (g *GitopsBot) TriggerNow() {
	select {
	case g.cronTrigger <- struct{}{}:
	default:
	}
}

func (g *GitopsBot) Stop() {
	close(g.stopTrigger)
}

func (g *GitopsBot) Setup() {
	g.stopTrigger = make(chan struct{})
	g.cronTrigger = make(chan struct{}, 1)
}

func (g *GitopsBot) Cron(ctx context.Context) {

	for {
		select {
		case <-g.stopTrigger:
			return
		case <-g.cronTrigger:
			err := g.Execute(ctx)
			g.Logger.IfErr(err).Warn(ctx, "unable to execute manual iteration of cron")
		case <-time.After(g.CronInterval):
			err := g.Execute(ctx)
			g.Logger.IfErr(err).Warn(ctx, "unable to execute iteration of cron")
		}
	}
}
