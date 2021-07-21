package filecontentchangemaker

import (
	"context"
	"fmt"
	"io"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type FileContentWorkingTreeChanger struct {
	ContentChangeCheck ContentChangeCheck
	Logger             *zapctx.Logger
	Cfg                autobotcfg.ChangeMakerConfig
	PerRepo            autobotcfg.PerRepoChangeMakerConfig
}

type ReadableFile interface {
	Name() string
	io.WriterTo
}

type gitFile struct {
	file *object.File
}

func (g *gitFile) WriteTo(w io.Writer) (n int64, retErr error) {
	r, err := g.file.Reader()
	if err != nil {
		return 0, fmt.Errorf("unable to get reader for file %s: %w", g.Name(), err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("unable to close opened file: %w", closeErr)
			} else {
				retErr = fmt.Errorf("also unable to close opened file %s: %w", closeErr.Error(), retErr)
			}
		}
	}()
	n, e := io.Copy(w, r)
	if e != nil {
		return n, fmt.Errorf("unale to copy file %s: %w", g.Name(), e)
	}
	return n, nil
}

func (g *gitFile) Name() string {
	return g.file.Name
}

var _ ReadableFile = &gitFile{}

func (f *FileContentWorkingTreeChanger) ChangeWorkingTree(w *git.Worktree, baseCommit *object.Commit, gitCommitter changemaker.GitCommitter) error {
	ctx := context.TODO()
	files, err := baseCommit.Files()
	if err != nil {
		return fmt.Errorf("unable to list files: %w", err)
	}
	var allChanges []ExpectedChange
	err = files.ForEach(func(file *object.File) error {
		if !f.PerRepo.MatchFile(file.Name) {
			return nil
		}
		gf := gitFile{file: file}
		fc, err := f.ContentChangeCheck.NewContent(ctx, &gf)
		if err != nil {
			return fmt.Errorf("unable to get new content for file %s: %w", file.Name, err)
		}
		if fc != nil {
			allChanges = append(allChanges, ExpectedChange{
				FileChange: *fc,
				FileName:   file.Name,
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to iterate each file: %w", err)
	}
	splits := splitChange(allChanges)
	if len(splits) == 0 {
		return nil
	}
	for idx, s := range splits {
		if err := w.Clean(&git.CleanOptions{Dir: true}); err != nil {
			return fmt.Errorf("unable to clean for new checkout: %w", err)
		}
		if err := w.Reset(&git.ResetOptions{
			Commit: baseCommit.Hash,
			Mode:   git.HardReset,
		}); err != nil {
			return fmt.Errorf("unable to reset after clean: %w", err)
		}
		if err := w.Checkout(&git.CheckoutOptions{
			Hash:   baseCommit.Hash,
			Branch: plumbing.NewBranchReferenceName(fmt.Sprintf("change_%d", idx)),
			Create: true,
		}); err != nil {
			return fmt.Errorf("unable to check out new branch: %w", err)
		}
		for _, c := range s.Changes {
			f, err := w.Filesystem.Create(c.FileName)
			if err != nil {
				return fmt.Errorf("unable to open file %s for write: %w", c.FileName, err)
			}
			if _, err := c.NewContent.WriteTo(f); err != nil {
				return fmt.Errorf("unable to write to file %s: %w", c.FileName, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("unable to close file %s: %w", c.FileName, err)
			}
			if _, err := w.Add(c.FileName); err != nil {
				return fmt.Errorf("unable to git add file %s: %w", c.FileName, err)
			}
		}
		if _, err := gitCommitter.Commit(w, s.CommitTitle+"\n\n"+s.CommitMessage, nil, f.Cfg, f.PerRepo); err != nil {
			return fmt.Errorf("unable to run get commit: %w", err)
		}
	}
	return nil
}

type GroupedChange struct {
	CommitTitle   string
	CommitMessage string
	GroupHash     string
	Changes       []SingleChange
}

type SingleChange struct {
	FileName   string
	NewContent io.WriterTo
}

func splitChange(ec []ExpectedChange) []GroupedChange {
	ret := make([]GroupedChange, 0, len(ec))
	changesByHash := make(map[string]*GroupedChange)
	for _, c := range ec {
		thisChange := SingleChange{
			FileName:   c.FileName,
			NewContent: c.NewContent,
		}
		if c.GroupHash == "" {
			ret = append(ret, GroupedChange{
				CommitTitle:   c.CommitTitle,
				CommitMessage: c.CommitMessage,
				GroupHash:     c.GroupHash,
				Changes:       []SingleChange{thisChange},
			})
			continue
		}
		if prev, exists := changesByHash[c.GroupHash]; exists {
			prev.Changes = append(prev.Changes, thisChange)
		} else {
			changesByHash[c.GroupHash] = &GroupedChange{
				CommitTitle:   c.CommitTitle,
				CommitMessage: c.CommitMessage,
				GroupHash:     c.GroupHash,
				Changes:       []SingleChange{thisChange},
			}
		}
	}
	for _, change := range changesByHash {
		ret = append(ret, *change)
	}
	return ret
}

type FileChange struct {
	NewContent    io.WriterTo
	CommitTitle   string
	CommitMessage string
	GroupHash     string
}

type ExpectedChange struct {
	FileChange
	FileName string
}

type ContentChangeCheck interface {
	NewContent(ctx context.Context, file ReadableFile) (*FileChange, error)
}

var _ changemaker.WorkingTreeChanger = &FileContentWorkingTreeChanger{}
