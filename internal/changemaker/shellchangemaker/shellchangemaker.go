package shellchangemaker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type ShellChangeMaker struct {
	ShellData         ShellData
	Logger            *zapctx.Logger
	AutoApprove       bool
	AutoMerge         bool
	ChangeMakerConfig autobotcfg.ChangeMakerConfig
	PerRepoConfig     autobotcfg.PerRepoChangeMakerConfig
}

type ShellData struct {
	Name    string        `yaml:"name"`
	Bin     string        `yaml:"bin"`
	Timeout time.Duration `yaml:"timeout"`
	Args    []string
}

func firstXChars(s string, x int) string {
	if len(s) <= x {
		return s
	}
	return s[:x]
}

func (s *ShellChangeMaker) branchName() string {
	filteredBranchName := "shellchange" + strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._", r) {
			return r
		}
		return '_'
	}, s.ShellData.Name)
	const maximumBranchSize = 100
	if len(filteredBranchName) > maximumBranchSize {
		filteredBranchName = filteredBranchName[0:maximumBranchSize]
	}
	return filteredBranchName
}

func (s *ShellChangeMaker) ChangeWorkingTree(w *git.Worktree, baseCommit *object.Commit, gitCommitter changemaker.GitCommitter, baseDir string) error {
	if err := w.Clean(&git.CleanOptions{Dir: true}); err != nil {
		return fmt.Errorf("unable to clean for new checkout: %w", err)
	}
	if err := w.Checkout(&git.CheckoutOptions{
		Hash:   baseCommit.Hash,
		Branch: plumbing.NewBranchReferenceName(s.branchName()),
		Create: true,
	}); err != nil {
		return fmt.Errorf("unable to check out new branch: %w", err)
	}
	if err := w.Reset(&git.ResetOptions{
		Commit: baseCommit.Hash,
		Mode:   git.HardReset,
	}); err != nil {
		return fmt.Errorf("unable to reset after clean: %w", err)
	}

	ctx := context.Background()
	if s.ShellData.Timeout != 0 {
		var onCancel context.CancelFunc
		ctx, onCancel = context.WithTimeout(ctx, s.ShellData.Timeout)
		defer onCancel()
	}
	//nolint:golint,gosec
	cmd := exec.CommandContext(ctx, s.ShellData.Bin, s.ShellData.Args...)
	cmd.Dir = baseDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Note: We can add extra env variables as we need them
	if err := cmd.Run(); err != nil {
		s.Logger.IfErr(err).Warn(ctx, "failed stdout", zap.String("stdout", firstXChars(stdout.String(), 128)), zap.String("stderr", firstXChars(stderr.String(), 128)))
		return fmt.Errorf("unable to execute required command: %w", err)
	}
	// Run git commit
	annotations := changemaker.CommitAnnotations{
		AutoMerge:   s.AutoMerge,
		AutoApprove: s.AutoApprove,
	}
	// Assume 'git add' is already run by the shell
	stat, err := w.Status()
	if err != nil {
		return fmt.Errorf("unable to run git status: %w", err)
	}
	if len(stat) == 0 {
		// No files changed or added
		return nil
	}
	s.Logger.Warn(ctx, "status of files", zap.Any("stat", stat))
	msg := fmt.Sprintf("shell command %s\n\nRan command %s", s.ShellData.Name, s.ShellData.Bin)
	if _, err := gitCommitter.Commit(w, msg, nil, s.ChangeMakerConfig, s.PerRepoConfig, &annotations); err != nil {
		return fmt.Errorf("unable to run git commit: %w", err)
	}
	return nil
}

var _ changemaker.WorkingTreeChanger = &ShellChangeMaker{}

func MakeFactory(z *zapctx.Logger) changemaker.WorkingTreeChangerFactory {
	return func(cmc autobotcfg.ChangeMakerConfig, prcmc autobotcfg.PerRepoChangeMakerConfig) ([]changemaker.WorkingTreeChanger, error) {
		if cmc.Name != "cmd" {
			return nil, nil
		}
		var buf bytes.Buffer
		if err := yaml.NewEncoder(&buf).Encode(cmc.Data); err != nil {
			return nil, fmt.Errorf("unable to encode change config: %w", err)
		}
		var datas []ShellData
		if err := yaml.NewDecoder(&buf).Decode(&datas); err != nil {
			return nil, fmt.Errorf("unable to re decode change config: %w", err)
		}
		z.Debug(context.Background(), "decoded data", zap.Any("data", datas))
		var ret []changemaker.WorkingTreeChanger
		for _, d := range datas {
			if d.Name != prcmc.Which {
				continue
			}
			ret = append(ret, &ShellChangeMaker{
				ShellData:         d,
				Logger:            z.With(zap.String("changer", "shellchangemaker")),
				PerRepoConfig:     prcmc,
				AutoApprove:       prcmc.AutoApprove,
				AutoMerge:         prcmc.AutoMerge,
				ChangeMakerConfig: cmc,
			})
		}
		if len(ret) == 0 {
			return nil, nil
		}
		return ret, nil
	}
}
