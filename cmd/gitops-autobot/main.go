package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport/client"

	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/cache"
	"github.com/cresta/gitops-autobot/internal/changemaker"
	"github.com/cresta/gitops-autobot/internal/changemaker/filecontentchangemaker/timechangemaker"
	"github.com/cresta/gitops-autobot/internal/checkout"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/gitops-autobot/internal/ghapp/cachedgithub"
	"github.com/cresta/gitops-autobot/internal/ghapp/githubdirect"
	"github.com/cresta/gitops-autobot/internal/gitopsbot"
	"github.com/cresta/gitops-autobot/internal/prcreator"
	"github.com/cresta/gitops-autobot/internal/prmerger"
	"github.com/cresta/gitops-autobot/internal/prreviewer"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/cresta/gotracing"
	"github.com/cresta/gotracing/datadog"
	"github.com/cresta/httpsimple"
	"github.com/cresta/zapctx"
	"github.com/gorilla/mux"
	"github.com/signalfx/golib/v3/httpdebug"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type config struct {
	ListenAddr      string
	DebugListenAddr string
	Tracer          string
	LogLevel        string
	ConfigFile      string
	CronInterval    time.Duration
}

func (c config) WithDefaults() config {
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	if c.DebugListenAddr == "" {
		c.DebugListenAddr = ":6060"
	}
	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}
	if c.ConfigFile == "" {
		c.ConfigFile = "gitops-autobot.yaml"
	}
	if c.CronInterval == 0 {
		c.CronInterval = time.Minute + time.Second*30
	}
	return c
}

func getConfig() config {
	return config{
		// Defaults to ":8080"
		ListenAddr: os.Getenv("LISTEN_ADDR"),
		// Defaults to ":6060"
		DebugListenAddr: os.Getenv("DEBUG_ADDR"),
		// Allows you to use a dynamic tracer
		Tracer: os.Getenv("TRACER"),
		// Level to log at
		LogLevel: os.Getenv("LOG_LEVEL"),
		// ConfigFile is the location of the gitops repo config file
		ConfigFile: os.Getenv("GITOPS_CONFIG_FILE"),
		// CronInterval is how frequently we make new pull requests
		CronInterval: fromDuration("CRON_INTERVAL"),
	}.WithDefaults()
}

func fromDuration(envKey string) time.Duration {
	envVal := os.Getenv(envKey)
	if envVal == "" {
		return 0
	}
	ret, err := time.ParseDuration(envVal)
	if err != nil {
		panic("invalid duration at " + envKey + ": " + err.Error())
	}
	return ret
}

func main() {
	instance.Main()
}

type Service struct {
	osExit    func(int)
	config    config
	log       *zapctx.Logger
	onListen  func(net.Listener)
	server    *http.Server
	tracers   *gotracing.Registry
	gitopsBot *gitopsbot.GitopsBot
}

var instance = Service{
	osExit: os.Exit,
	config: getConfig(),
	tracers: &gotracing.Registry{
		Constructors: map[string]gotracing.Constructor{
			"datadog": datadog.NewTracer,
		},
	},
}

func setupLogging(logLevel string) (*zapctx.Logger, error) {
	zapCfg := zap.NewProductionConfig()
	var lvl zapcore.Level
	logLevelErr := lvl.UnmarshalText([]byte(logLevel))
	if logLevelErr == nil {
		zapCfg.Level.SetLevel(lvl)
	}
	l, err := zapCfg.Build(zap.AddCaller())
	if err != nil {
		return nil, err
	}
	retLogger := zapctx.New(l)
	retLogger.IfErr(logLevelErr).Warn(context.Background(), "unable to parse log level")
	return retLogger, nil
}

func (m *Service) Main() {
	cfg := m.config
	if m.log == nil {
		var err error
		m.log, err = setupLogging(m.config.LogLevel)
		if err != nil {
			fmt.Printf("Unable to setup logging: %v", err)
			m.osExit(1)
			return
		}
	}
	m.log.Info(context.Background(), "Starting", zap.Any("config", m.config))
	rootTracer, err := m.tracers.New(m.config.Tracer, gotracing.Config{
		Log: m.log.With(zap.String("section", "setup_tracing")),
		Env: os.Environ(),
	})
	if err != nil {
		m.log.IfErr(err).Error(context.Background(), "unable to setup tracing")
		m.osExit(1)
		return
	}

	ctx := context.Background()
	m.log = m.log.DynamicFields(rootTracer.DynamicFields()...)
	if err := m.injection(ctx, rootTracer); err != nil {
		m.log.IfErr(err).Panic(ctx, "unable to inject starting variables")
		m.osExit(1)
		return
	}

	m.server = m.setupServer(cfg, m.log, rootTracer)
	shutdownCallback, err := setupDebugServer(m.log, cfg.DebugListenAddr, m)
	if err != nil {
		m.log.IfErr(err).Panic(context.Background(), "unable to setup debug server")
		m.osExit(1)
		return
	}
	m.gitopsBot.Setup()
	go m.gitopsBot.Cron(ctx)
	serveErr := httpsimple.BasicServerRun(m.log, m.server, m.onListen, m.config.ListenAddr)
	m.gitopsBot.Stop()
	shutdownCallback()
	if serveErr != nil {
		m.osExit(1)
	}
}

func (m *Service) injection(ctx context.Context, tracer gotracing.Tracing) error {
	client.InstallProtocol("https", githttp.NewClient(&http.Client{
		Transport: tracer.WrapRoundTrip(http.DefaultTransport),
	}))
	client.InstallProtocol("http", githttp.NewClient(&http.Client{
		Transport: tracer.WrapRoundTrip(http.DefaultTransport),
	}))
	f, err := os.Open(m.config.ConfigFile)
	if err != nil {
		return fmt.Errorf("unable to open file %s: %w", m.config.ConfigFile, err)
	}
	defer func() {
		m.log.IfErr(f.Close()).Error(ctx, "unable to close opened config file")
	}()
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, f); err != nil {
		return fmt.Errorf("unable to copy from config file: %w", err)
	}
	cfg, err := autobotcfg.Load(&buf)
	if err != nil {
		return fmt.Errorf("unable to load config file: %w", err)
	}
	committer, err := changemaker.CommitterFromConfig(cfg.CommitterConfig)
	if err != nil {
		return fmt.Errorf("unable to load committer from config: %w", err)
	}
	memoryCache := &cache.InMemoryCache{}
	directPRCreatorClient, err := githubdirect.NewFromConfig(ctx, cfg.PRCreator, tracer.WrapRoundTrip(http.DefaultTransport), m.log)
	if err != nil {
		return fmt.Errorf("unable to make direct github client: %w", err)
	}
	cachedPRCreatorClient := &cachedgithub.CachedGithub{
		Into:  directPRCreatorClient,
		Cache: memoryCache,
	}
	prMaker, err := cachedPRCreatorClient.Self(ctx)
	if err != nil {
		return fmt.Errorf("unable to find self for pr creator: %w", err)
	}
	directPRReviewerClient, err := githubdirect.NewFromConfig(ctx, *cfg.PRReviewer, tracer.WrapRoundTrip(http.DefaultTransport), m.log)
	if err != nil {
		return fmt.Errorf("unable to make direct github client: %w", err)
	}
	cachedPRReviewerClient := &cachedgithub.CachedGithub{
		Into:  directPRReviewerClient,
		Cache: memoryCache,
	}
	cfg, err = ghapp.PopulateRepoDefaultBranches(ctx, cfg, cachedPRCreatorClient)
	if err != nil {
		return fmt.Errorf("unable to populate default branches: %w", err)
	}
	allCheckouts := make([]*checkout.Checkout, 0, len(cfg.Repos))
	for _, repo := range cfg.Repos {
		co, err := checkout.NewCheckout(ctx, m.log, repo, cfg.CloneDataDir, cachedPRCreatorClient.GoGetAuthMethod())
		if err != nil {
			return fmt.Errorf("unable to setup checkout: %w", err)
		}
		allCheckouts = append(allCheckouts, co)
	}
	factory := changemaker.Factory{
		Factories: []changemaker.WorkingTreeChangerFactory{
			timechangemaker.Factory,
		},
	}
	prCreator := &prcreator.PrCreator{
		F:             &factory,
		AutobotConfig: cfg,
		Logger:        m.log,
		GitCommitter:  committer,
		Client:        cachedPRCreatorClient,
	}
	prMerger := &prmerger.PRMerger{
		AutobotConfig: cfg,
		Client:        cachedPRReviewerClient,
		Logger:        m.log,
	}
	prReviewer := &prreviewer.PrReviewer{
		AutobotConfig: cfg,
		Logger:        m.log,
		Client:        cachedPRReviewerClient,
		PRMaker:       prMaker,
	}
	m.gitopsBot = &gitopsbot.GitopsBot{
		PRCreator:    prCreator,
		PrReviewer:   prReviewer,
		PRMerger:     prMerger,
		Checkouts:    allCheckouts,
		Tracer:       tracer,
		Logger:       m.log.With(zap.String("class", "gitopsbot")),
		CronInterval: m.config.CronInterval,
	}
	return nil
}

func (m *Service) setupServer(cfg config, log *zapctx.Logger, tracer gotracing.Tracing) *http.Server {
	rootHandler := mux.NewRouter()
	rootHandler.Handle("/health", httpsimple.HealthHandler(log, tracer))
	rootHandler.Methods(http.MethodPost).Path("/trigger").HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		m.gitopsBot.TriggerNow()
		writer.WriteHeader(http.StatusAccepted)
		_, err := io.WriteString(writer, "triggered async")
		m.log.IfErr(err).Warn(request.Context(), "unable to write out status")
	})
	return &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: rootHandler,
	}
}

func setupDebugServer(l *zapctx.Logger, listenAddr string, obj interface{}) (func(), error) {
	if listenAddr == "" || listenAddr == "-" {
		return func() {
		}, nil
	}
	ret := httpdebug.New(&httpdebug.Config{
		Logger:        &zapctx.FieldLogger{Logger: l},
		ExplorableObj: obj,
	})
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("unable to listen to %s: %w", listenAddr, err)
	}
	go func() {
		serveErr := ret.Server.Serve(ln)
		if serveErr != http.ErrServerClosed {
			l.IfErr(serveErr).Error(context.Background(), "debug server existed")
		}
		l.Info(context.Background(), "debug server finished")
	}()
	return func() {
		err := ln.Close()
		l.IfErr(err).Warn(context.Background(), "unable to close listening socket for debug server")
	}, nil
}
