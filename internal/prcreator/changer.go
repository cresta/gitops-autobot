package prcreator

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/gogitwrap"
	"strings"
	"time"
)

type ChangerFactory interface {
	Changers(ctx context.Context, configFile []byte) ([]Changer, error)
}

type File = gogitwrap.File

type Changer interface {
	ChangeFile(ctx context.Context, file File) (*ChangeRequest, error)
	Desc() string
	CommitMessage(context.Context, []*ChangeRequest) (string, error)
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

func (c *TimeChanger) CommitMessage(_ context.Context, requests []*ChangeRequest) (string, error) {
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