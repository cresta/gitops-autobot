package prcreator

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	http2 "net/http"
	"os"
	"testing"

	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker/helmchangemaker"
	"github.com/cresta/gitops-autobot/internal/versionfetch/helm"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/cache"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker/timechangemaker"
	"github.com/cresta/gitops-autobot/internal/checkout"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/gitops-autobot/internal/ghapp/cachedgithub"
	"github.com/cresta/gitops-autobot/internal/ghapp/githubdirect"
	"github.com/cresta/zapctx/testhelp/testhelp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPrCreator_Execute(t *testing.T) {
	td, err := ioutil.TempDir("", "TestPrCreator_Execute")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(td))
	}()
	ctx := context.Background()
	logger := testhelp.ZapTestingLogger(t)
	factory := changemaker.Factory{
		Factories: []changemaker.WorkingTreeChangerFactory{
			timechangemaker.Factory, helmchangemaker.MakeFactory(&helm.RepoInfoLoader{
				Cache:  &cache.InMemoryCache{},
				Client: http2.DefaultClient,
				Logger: logger,
				LoadersByScheme: map[string]helm.IndexLoader{
					"https": &helm.HTTPLoader{
						Logger: logger,
						Client: http2.DefaultClient,
					},
					"http": &helm.HTTPLoader{
						Logger: logger,
						Client: http2.DefaultClient,
					},
				},
			}, &helm.ChangeParser{
				Logger: logger,
			}, logger),
		},
	}
	testRepoCfg := "test-repo-config.yaml"
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
	cfg.CloneDataDir = td
	logger.Info(ctx, "loaded config", zap.Any("config", cfg))

	committer, err := changemaker.CommitterFromConfig(cfg.CommitterConfig)
	require.NoError(t, err)
	directClient, err := githubdirect.NewFromConfig(ctx, cfg.PRCreator, http2.DefaultTransport, logger)
	require.NoError(t, err)
	client := &cachedgithub.CachedGithub{
		Into:  directClient,
		Cache: &cache.InMemoryCache{},
	}
	cfg, err = ghapp.PopulateRepoDefaultBranches(ctx, cfg, client)
	require.NoError(t, err)

	allCheckouts := make([]*checkout.Checkout, 0, len(cfg.Repos))
	for _, repo := range cfg.Repos {
		co, err := checkout.NewCheckout(ctx, logger, repo, cfg.CloneDataDir, client.GoGetAuthMethod())
		require.NoError(t, err)
		allCheckouts = append(allCheckouts, co)
	}

	require.NoError(t, err)

	pr := PrCreator{
		F:             &factory,
		AutobotConfig: cfg,
		Logger:        logger,
		GitCommitter:  committer,
		Client:        client,
	}
	for _, co := range allCheckouts {
		require.NoError(t, pr.Execute(ctx, co))
	}
}
