package prcreator

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/gogitwrap"
	"github.com/go-git/go-git/v5"
	"strings"
	"time"
)

type ChangerFactory interface {
	Changers(ctx context.Context, configFile []byte) ([]Changer, error)
}

type File = gogitwrap.File

type ChangeGroup struct {
	CommitMessage string
	GroupHash string
}

type Changer interface {
	DoChanges(ctx context.Context, worktree git.Worktree) (*ChangeGroup, error)
}

type FileChange interface {
	ChangeFile(ctx context.Context, file File) (*ChangeRequest, error)
}

func T(w git.Worktree) {
	w.Filesystem.Open()
}

type PerFileChanger struct {

}

type ChangeRequest struct {
	NewContent []byte
	GroupHash string
	Metadata interface{}
}

// TimeChanger will find lines that begin with "time=" and replaces them with "time=X" where X is the current time
type TimeChanger struct {
}

func (c *TimeChanger) Desc() string {
	return "now_time"
}

func (c *TimeChanger) CommitMessage(_ context.Context, requests []*FileChange) (string, error) {
	return fmt.Sprintf("time change\nChanging %d files", len(requests)), nil
}

var _ Changer = &TimeChanger{}

func (c *TimeChanger) ChangeFile(_ context.Context, file File) (*ChangeRequest, error) {
	if !strings.HasSuffix(file.Name(), ".yaml") {
		return nil, nil
	}
	var buf bytes.Buffer
	_, err := file.WriteTo(&buf)
	if err != nil {
		return nil, fmt.Errorf("unable to read file content: %w", err)
	}
	lines := strings.Split(buf.String(), "\n")
	hasLine := false
	var newContent []string
	for _, line := range lines {
		if strings.HasPrefix(line, "time=") {
			newContent = append(newContent, "time=" + time.Now().String())
			hasLine = true
		} else {
			newContent = append(newContent, line)
		}
	}
	if !hasLine {
		return nil, nil
	}
	return &ChangeRequest{
		NewContent: []byte(strings.Join(newContent, "\n")),
		GroupHash: "",
	}, nil
}