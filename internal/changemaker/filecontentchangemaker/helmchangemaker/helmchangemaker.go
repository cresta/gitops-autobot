package helmchangemaker

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker"
	"github.com/cresta/gitops-autobot/internal/versionfetch/helm"
	"github.com/cresta/zapctx"
	"go.uber.org/zap"
)

type HelmChangeMaker struct {
	RepoInfoLoader *helm.RepoInfoLoader
	Parser         *helm.ChangeParser
	Logger         *zapctx.Logger
}

func (h *HelmChangeMaker) NewContent(ctx context.Context, file filecontentchangemaker.ReadableFile) (*filecontentchangemaker.FileChange, error) {
	var buf bytes.Buffer
	if _, err := file.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("uable to read content of file %s: %w", file.Name(), err)
	}
	lines := strings.Split(buf.String(), "\n")
	changes, err := helm.ParseHelmReleaseYAML(lines)
	if err != nil {
		return nil, fmt.Errorf("unable to parse lines of file %s: %w", file.Name(), err)
	}
	byRepo := helm.GroupChangesByRepo(changes)
	hasChange := false
	changeCommitMsg := ""
	autoMerge := false
	autoApprove := false
	for repoURL, changesByRepo := range byRepo {
		idxFile, err := h.RepoInfoLoader.LoadIndexFile(ctx, repoURL)
		if err != nil {
			return nil, fmt.Errorf("unable to load index file %s: %w", repoURL, err)
		}
		for _, change := range changesByRepo {
			thisChange, err := h.Parser.LoadVersions(ctx, change, idxFile)
			if err != nil {
				return nil, fmt.Errorf("unable to parse versions: %w", err)
			}
			if thisChange == nil {
				continue
			}
			h.Logger.Debug(ctx, "line change", zap.String("old", change.CurrentVersionLine), zap.String("new", thisChange.NewLine))
			if change.UpgradeInfo.AutoMerge != nil {
				autoMerge = autoMerge || *change.UpgradeInfo.AutoMerge
			}
			if change.UpgradeInfo.AutoApprove != nil {
				autoApprove = autoApprove || *change.UpgradeInfo.AutoApprove
			}
			changeCommitMsg += fmt.Sprintf("Changed %s %s => %s\n", change.UpgradeInfo.ChartName, change.UpgradeInfo.CurrentVersion, thisChange.NewVersion)
			lines[thisChange.LineNumber] = thisChange.NewLine
			hasChange = true
		}
	}
	if hasChange {
		return &filecontentchangemaker.FileChange{
			NewContent:    strings.NewReader(strings.Join(lines, "\n")),
			CommitTitle:   "Deploying new helm version",
			CommitMessage: changeCommitMsg,
			GroupHash:     "",
			AutoMerge:     autoMerge,
			AutoApprove:   autoApprove,
		}, nil
	}
	return nil, nil
}

func MakeFactory(repoInfoLoader *helm.RepoInfoLoader, parser *helm.ChangeParser, logger *zapctx.Logger) changemaker.WorkingTreeChangerFactory {
	return func(cfg autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) ([]changemaker.WorkingTreeChanger, error) {
		if cfg.Name != "helm" {
			return nil, nil
		}
		var timeConfig HelmChangeMaker
		if err := changemaker.ReEncodeYAML(perRepo.Data, &timeConfig); err != nil {
			return nil, fmt.Errorf("unable to decode time plugin config: %w", err)
		}
		return []changemaker.WorkingTreeChanger{
			&filecontentchangemaker.FileContentWorkingTreeChanger{
				Cfg:     cfg,
				PerRepo: perRepo,
				ContentChangeCheck: &HelmChangeMaker{
					Parser:         parser,
					Logger:         logger,
					RepoInfoLoader: repoInfoLoader,
				},
			},
		}, nil
	}
}

var _ filecontentchangemaker.ContentChangeCheck = &HelmChangeMaker{}
