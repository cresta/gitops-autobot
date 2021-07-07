package ghapp

import (
	"context"
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	http2 "net/http"
)

func NewFromConfig(ctx context.Context, cfg autobotcfg.GithubAppConfig, rt http2.RoundTripper) (*ghinstallation.Transport, error) {
	trans, err := ghinstallation.NewKeyFromFile(rt, cfg.AppID, cfg.InstallationID, cfg.PEMKeyLoc)
	if err != nil {
		return nil, fmt.Errorf("unable to find key file: %w", err)
	}
	_, err = trans.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to validate token: %w", err)
	}
	return trans, nil
}

type DynamicHttpAuthMethod struct {
	Itr    *ghinstallation.Transport
	Logger *zapctx.Logger
}

const ghAppUserName = "x-access-token"

func (d *DynamicHttpAuthMethod) String() string {
	return fmt.Sprintf("%s - %s:%s", d.Name(), ghAppUserName, "******")
}

func (d *DynamicHttpAuthMethod) Name() string {
	return "dynamic-http-basic-auth"
}

func (d *DynamicHttpAuthMethod) SetAuth(r *http2.Request) {
	if d == nil {
		return
	}
	tok, err := d.Itr.Token(r.Context())
	if err != nil {
		d.Logger.IfErr(err).Error(r.Context(), "unable to get github token")
		return
	}
	r.SetBasicAuth(ghAppUserName, tok)
}

var _ http.AuthMethod = &DynamicHttpAuthMethod{}
