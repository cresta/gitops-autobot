package prmerger

import (
	"bytes"
	"context"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx/testhelp/testhelp"
	"github.com/google/go-github/v29/github"
	"github.com/hasura/go-graphql-client"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"io"
	http2 "net/http"
	"os"
	"testing"
)

func TestGraphQL(t *testing.T) {
	ctx := context.Background()
	logger := testhelp.ZapTestingLogger(t)
	testRepoCfg := "../prcreator/test-repo-config.yaml"
	if _, err := os.Stat(testRepoCfg); os.IsNotExist(err) {
		t.Skipf("Unable to find testing repo config file %s", testRepoCfg)
	}
	f, err := os.Open(testRepoCfg)
	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, f)
	require.NoError(t, copyErr)
	require.NoError(t, err)
	cfg, err := autobotcfg.Load(&buf)
	require.NoError(t, err)
	logger.Info(ctx, "loaded config", zap.Any("config", cfg))
	trans, err := ghapp.NewFromConfig(ctx, *cfg.PRReviewer, http2.DefaultTransport)
	require.NoError(t, err)
	client := graphql.NewClient("https://api.github.com/graphql", &http2.Client{Transport: trans})
	var query struct {
		Viewer struct {
			Login string
		}
	}
	err2 := client.Query(ctx, &query, nil)
	require.NoError(t, err2)
	logger.Debug(ctx, "ran query", zap.Any("res", &query))

	var query2 struct {
	}
}

func TestPRMerger_Execute(t *testing.T) {
	ctx := context.Background()
	logger := testhelp.ZapTestingLogger(t)
	testRepoCfg := "../prcreator/test-repo-config.yaml"
	if _, err := os.Stat(testRepoCfg); os.IsNotExist(err) {
		t.Skipf("Unable to find testing repo config file %s", testRepoCfg)
	}
	f, err := os.Open(testRepoCfg)
	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, f)
	require.NoError(t, copyErr)
	require.NoError(t, err)
	cfg, err := autobotcfg.Load(&buf)
	require.NoError(t, err)
	logger.Info(ctx, "loaded config", zap.Any("config", cfg))

	if cfg.PRReviewer == nil {
		t.Log("no reviewer config set.  Skipping test")
	}

	trans, err := ghapp.NewFromConfig(ctx, *cfg.PRReviewer, http2.DefaultTransport)
	require.NoError(t, err)
	client := github.NewClient(&http2.Client{Transport: trans})
	require.NoError(t, err)

	pr := PRMerger{
		AutobotConfig: cfg,
		Logger:        logger,
		Client:        client,
	}
	require.NoError(t, pr.Execute(ctx))
}
