package shellchangemaker

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/checkout"
	"github.com/cresta/zapctx/testhelp/testhelp"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

type LocalConfig struct {
	Path string
}

func (l LocalConfig) String() string {
	return l.Path
}

func (l LocalConfig) CloneURL() string {
	return l.Path
}

func (l LocalConfig) RemoteBranch() string {
	return "master"
}

func (l LocalConfig) RemoteOwner() string {
	panic("Not allowed to call")
}

func (l LocalConfig) RemoteName() string {
	panic("Not allowed to call")
}

var _ checkout.RepoConfig = &LocalConfig{}

func createHelloWorldRepo(t *testing.T, dir string) (repo *git.Repository) {
	repo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	f, err := wt.Filesystem.Create("README.md")
	require.NoError(t, err)
	_, err = io.Copy(f, strings.NewReader("basic content"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	_, err = wt.Add("README.md")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john.doe@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)
	return repo
}

func TestShellChangeMaker(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("Skipping test because cannot find go binary: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("Skipping test because cannot find git binary: %v", err)
	}
	var fileBinary string
	if runtime.GOOS == "windows" {
		fileBinary = "makertest.bat"
	} else {
		fileBinary = "makertest.sh"
	}
	td, err := ioutil.TempDir("", "TestShellChangeMaker")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(td))
	}()
	ctx := context.Background()
	logger := testhelp.ZapTestingLogger(t)
	_ = createHelloWorldRepo(t, td)
	co, err := checkout.NewCheckout(ctx, logger, LocalConfig{Path: td}, "", nil)
	require.NoError(t, err)
	wt, baseCmt, err := co.SetupForWorkingTreeChanger(ctx)
	require.NoError(t, err)
	committer, err := changemaker.CommitterFromConfig(autobotcfg.CommitterConfig{
		AuthorName:  "John Doe",
		AuthorEmail: "john.doe@example.com",
	})
	require.NoError(t, err)
	testFileOne, err := filepath.Abs(fileBinary)
	require.NoError(t, err)
	s := ShellChangeMaker{
		ShellData: ShellData{
			Name:    "makertest",
			Bin:     testFileOne,
			Timeout: 0,
		},
		Logger: logger,
	}

	require.NoError(t, s.ChangeWorkingTree(wt, baseCmt, committer, td))
	b, err := ioutil.ReadFile(filepath.Join(td, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(b), "module example.com")
}
