package helmchangemaker

import (
	"bytes"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker"
)

type HelmChangeMaker struct {
}

func (h *HelmChangeMaker) NewContent(file filecontentchangemaker.ReadableFile) (*filecontentchangemaker.FileChange, error) {
	var buf bytes.Buffer
	if _, err := file.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("uable to read content of file %s: %w", file.Name(), err)
	}

}

func Factory(cfg autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) ([]changemaker.WorkingTreeChanger, error) {
	if cfg.Name != "helm" {
		return nil, nil
	}
	var timeConfig HelmChangeMaker
	if err := changemaker.ReEncodeYAML(perRepo.Data, &timeConfig); err != nil {
		return nil, fmt.Errorf("unable to decode time plugin config: %w", err)
	}
	return []changemaker.WorkingTreeChanger{
		&filecontentchangemaker.FileContentWorkingTreeChanger{
			Cfg:                cfg,
			PerRepo:            perRepo,
			ContentChangeCheck: &HelmChangeMaker{},
		},
	}, nil
}

var _ changemaker.WorkingTreeChangerFactory = Factory

var _ filecontentchangemaker.ContentChangeCheck = &HelmChangeMaker{}
