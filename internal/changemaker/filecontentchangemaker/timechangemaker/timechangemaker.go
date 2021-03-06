package timechangemaker

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker"
)

type TimeWorkingTreeChanger struct {
	Data TimeWorkingTreeChangerData
}

type TimeWorkingTreeChangerData struct {
	Format string        `yaml:"format"`
	Round  time.Duration `yaml:"roundTo"`
}

const defaultLayout = "2006-01-02 15:04:05.999999999 -0700 MST"

func (t *TimeWorkingTreeChanger) NewContent(_ context.Context, file filecontentchangemaker.ReadableFile) (*filecontentchangemaker.FileChange, error) {
	var buf bytes.Buffer
	if _, err := file.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("unable to read from file %s: %w", file.Name(), err)
	}
	now := time.Now().UTC()
	lines := strings.Split(buf.String(), "\n")
	hasChange := false
	format := t.Data.Format
	if format == "" {
		format = defaultLayout
	}
	if t.Data.Round != 0 {
		now = now.Truncate(t.Data.Round)
	}
	for idx, line := range lines {
		if strings.HasPrefix(line, "time=") {
			newLine := "time=" + now.Format(format)
			if lines[idx] == newLine {
				continue
			}
			hasChange = true
			lines[idx] = newLine
		}
	}
	if hasChange {
		return &filecontentchangemaker.FileChange{
			NewContent:    strings.NewReader(strings.Join(lines, "\n")),
			CommitTitle:   "time update",
			CommitMessage: "Updated time to " + now.String(),
			GroupHash:     "time",
		}, nil
	}
	return nil, nil
}

func Factory(cfg autobotcfg.ChangeMakerConfig, perRepo autobotcfg.PerRepoChangeMakerConfig) ([]changemaker.WorkingTreeChanger, error) {
	if cfg.Name != "time" {
		return nil, nil
	}
	var timeConfig TimeWorkingTreeChangerData
	if err := changemaker.ReEncodeYAML(perRepo.Data, &timeConfig); err != nil {
		return nil, fmt.Errorf("unable to decode time plugin config: %w", err)
	}
	return []changemaker.WorkingTreeChanger{
		&filecontentchangemaker.FileContentWorkingTreeChanger{
			Cfg:     cfg,
			PerRepo: perRepo,
			ContentChangeCheck: &TimeWorkingTreeChanger{
				Data: timeConfig,
			},
		},
	}, nil
}

var _ changemaker.WorkingTreeChangerFactory = Factory

var _ filecontentchangemaker.ContentChangeCheck = &TimeWorkingTreeChanger{}
