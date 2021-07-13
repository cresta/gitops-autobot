package prmerger

import (
	"bytes"
	"context"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/cache"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/gitops-autobot/internal/ghapp/cachedgithub"
	"github.com/cresta/gitops-autobot/internal/ghapp/githubdirect"
	"github.com/cresta/zapctx/testhelp/testhelp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"io"
	http2 "net/http"
	"os"
	"testing"
)

func TestPRMerger_Execute(t *testing.T) {
	ctx := context.Background()
	logger := testhelp.ZapTestingLogger(t)
	testRepoCfg := "../prcreator/test-repo-config.yaml"
	if _, err := os.Stat(testRepoCfg); os.IsNotExist(err) {
		t.Skipf("Unable to find testing repo config file %s", testRepoCfg)
	}
	f, err := os.Open(testRepoCfg)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()
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

	directClient, err := githubdirect.NewFromConfig(ctx, *cfg.PRReviewer, http2.DefaultTransport, logger)
	require.NoError(t, err)
	client := &cachedgithub.CachedGithub{
		Into:  directClient,
		Cache: &cache.InMemoryCache{},
	}
	cfg, err = ghapp.PopulateRepoDefaultBranches(ctx, cfg, client)
	require.NoError(t, err)

	pr := PRMerger{
		AutobotConfig: cfg,
		Client:        client,
		Logger:        logger,
	}
	require.NoError(t, pr.Execute(ctx))
}
