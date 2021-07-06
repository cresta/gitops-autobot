package timechangemaker

import (
	"bytes"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker"
	"strings"
	"time"
)

type TimeWorkingTreeChanger struct {
	Cfg autobotcfg.PerRepoChangeMakerConfig
}

func (t *TimeWorkingTreeChanger) NewContent(file filecontentchangemaker.ReadableFile) (*filecontentchangemaker.FileChange, error) {
	if !t.Cfg.MatcheFile(file.Name()) {
		return nil, nil
	}
	var buf bytes.Buffer
	if _, err := file.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("unable to read from file %s: %w", file.Name(), err)
	}
	now := time.Now()
	lines := strings.Split(buf.String(), "\n")
	hasChange := false
	for idx, line := range lines {
		if strings.HasPrefix(line, "time=") {
			hasChange = true
			lines[idx] = "time=" + now.String()
		}
	}
	if hasChange {
		return &filecontentchangemaker.FileChange{
			NewContent:    nil,
			CommitTitle:   "time update",
			CommitMessage: "Updated time to " + now.String(),
			GroupHash:     "time",
		}, nil
	}
	return nil, nil
}

func TimeChangeMakerFactory(cfg autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) ([]changemaker.WorkingTreeChanger, error) {
	if cfg.Name != "time" {
		return nil, nil
	}
	return []changemaker.WorkingTreeChanger{
		&filecontentchangemaker.FileContentWorkingTreeChanger{
			ContentChangeCheck: &TimeWorkingTreeChanger{
				Cfg: perRepo,
			},
		},
	}, nil
}

var _ changemaker.WorkingTreeChangerFactory = TimeChangeMakerFactory

var _ filecontentchangemaker.ContentChangeCheck = &TimeWorkingTreeChanger{}
