package prcreator

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/checkout"
	"github.com/cresta/zapctx"
	"github.com/google/go-github/v29/github"
)

type PrCreator struct {
	F             *changemaker.Factory
	AutobotConfig autobotcfg.AutobotConfig
	Logger        zapctx.Logger
	GitCommitter  changemaker.GitCommitter
	Client        *github.Client
}

func (p *PrCreator) Execute(ctx context.Context, checkout *checkout.Checkout, changers []changemaker.WorkingTreeChanger) error {
	if err := checkout.Refresh(ctx); err != nil {
		return fmt.Errorf("unable to refresh repo: %w", err)
	}
	if err := checkout.Clean(ctx); err != nil {
		return fmt.Errorf("unable to clean repo: %w", err)
	}
	cfg, err := checkout.CurrentConfig()
	if err != nil {
		return fmt.Errorf("unable to get current config: %w", err)
	}
	if cfg == nil {
		p.Logger.Debug(ctx, "no config for this repo")
		return nil
	}
	changers, err2 := p.F.Load(p.AutobotConfig.ChangeMakers, *cfg)
	if err2 != nil {
		return fmt.Errorf("unable to load changers: %w", err2)
	}
	for _, c := range changers {
		if err := checkout.Clean(ctx); err != nil {
			return fmt.Errorf("unable to clean repo: %w", err)
		}
		wt, obj, err := checkout.SetupForWorkingTreeChanger()
		if err != nil {
			return fmt.Errorf("unable to setup working tree: %w", err)
		}
		if err := c.ChangeWorkingTree(wt, obj, p.GitCommitter); err != nil {
			return fmt.Errorf("unable to change working tree: %w", err)
		}
	}
	if err := checkout.PushAllNewBranches(ctx, p.Client); err != nil {
		return fmt.Errorf("unable to push new branches: %w", err)
	}
	return nil
}
