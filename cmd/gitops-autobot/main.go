package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

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
	return c
}

func getConfig() config {
	return config{
		// Defaults to ":8080"
		ListenAddr: os.Getenv("LISTEN_ADDR"),
		// Defaults to ":6060"
		DebugListenAddr: os.Getenv("DEBUG_ADDR"),
		// Allows you to use a dynamic tracer
		Tracer:   os.Getenv("TRACER"),
		LogLevel: os.Getenv("LOG_LEVEL"),
	}.WithDefaults()
}

func main() {
	instance.Main()
}

type Service struct {
	osExit   func(int)
	config   config
	log      *zapctx.Logger
	onListen func(net.Listener)
	server   *http.Server
	tracers  *gotracing.Registry
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
	if err := m.injection(ctx); err != nil {
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
	serveErr := httpsimple.BasicServerRun(m.log, m.server, m.onListen, m.config.ListenAddr)

	shutdownCallback()
	if serveErr != nil {
		m.osExit(1)
	}
}

func (m *Service) injection(ctx context.Context) error {
	return nil
}

func (m *Service) setupServer(cfg config, log *zapctx.Logger, tracer gotracing.Tracing) *http.Server {
	rootHandler := mux.NewRouter()
	rootHandler.Handle("/health", httpsimple.HealthHandler(log, tracer))
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