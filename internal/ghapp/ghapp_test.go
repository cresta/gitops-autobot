package ghapp

import (
	"context"
	"github.com/cresta/gitops-autobot/internal/gogitwrap"
	"github.com/cresta/gotracing"
	"github.com/cresta/zapctx/testhelp/testhelp"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v29/github"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
)

const testTokenName = "../../testing-token.pem"

type testConfig struct {
	AppID int64
	InstallationID int64
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cleanupRepo(t *testing.T, c *gogitwrap.GitCheckout) {
	require.NotEqual(t, "/", c.AbsPath())
	require.NotEmpty(t, c.AbsPath())
	require.True(t, strings.HasPrefix(c.AbsPath(), os.TempDir()))
	t.Log("Deleting all of", c.AbsPath())
	require.NoError(t, os.RemoveAll(c.AbsPath()))
}

func TestToken(t *testing.T) {
	ctx := context.Background()
	if !fileExists(testTokenName) {
		t.Skipf("unable to find testing token file %s", testTokenName)
	}
	gh := GhApp{
		AppID:          1,
		InstallationID: 2,
		PEMKeyLoc:      testTokenName,
	}
	tok, err := gh.Token(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	g := gogitwrap.Git{
		Log:    testhelp.ZapTestingLogger(t),
		Tracer: gotracing.Noop{},
	}
	into, err := ioutil.TempDir("", "TestToken")
	require.NoError(t, err)
	require.NotEmpty(t, into)
	client := github.NewClient(&http.Client{Transport: gh.Itr})

	repo := "https://github.com/cresta/gitdb-reference.git"
	gc, err := g.Clone(ctx, into, repo, &githttp.BasicAuth{
		Username: "x-access-token", // anything except an empty string
		Password: tok,
	})
	require.NoError(t, err)
	t.Log("We are at", into)
	//_ = gc
	//defer cleanupRepo(t, gc)
	require.NoError(t, ioutil.WriteFile(into + "/on_master.txt", []byte("test_pr"), 0777))
	gc.SetDefaultAuthor("integration_test", "no-reply@cresta.ai")
	require.NoError(t, gc.CommitAndPush(ctx, "Integration test", "int_test"))

	pr, resp, err := client.PullRequests.Create(ctx, "cresta", "gitdb-reference", &github.NewPullRequest{
		Title:               github.String("hello world"),
		Head:                github.String("test_pr6"),
		Base:                github.String("master"),
	})
	require.NoError(t, err)
	t.Log("status code", resp.StatusCode)
	t.Log("pr", *pr.ID)

	res, resp2, err := client.PullRequests.Merge(ctx, "cresta", "gitdb-reference", *pr.Number, "", &github.PullRequestOptions{
		MergeMethod: "squash",
	})
	require.NoError(t, err)
	t.Log("result: ", res.GetMerged())
	t.Log("resp", resp2.StatusCode)
	//prr, _, err := client.PullRequests.CreateReview(ctx, "cresta", "gitdb-reference", 6, &github.PullRequestReviewRequest{
	//	Event:    github.String("APPROVE"),
	//})
	//require.NoError(t, err)
	//_ = prr
	//_, _, err = client.PullRequests.SubmitReview(ctx, "cresta", "gitdb-reference", 6, *prr.ID, &github.PullRequestReviewRequest{
	//	Event:    github.String("APPROVE"),
	//})
	//require.NoError(t, err)
}
