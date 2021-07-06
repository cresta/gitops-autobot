package ghapp

import (
	"context"
	"fmt"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	http2 "net/http"
)
import "github.com/bradleyfalzon/ghinstallation"

type GhApp struct {
	AppID          int64
	InstallationID int64
	PEMKeyLoc      string
	Itr            *ghinstallation.Transport
}

func (g *GhApp) Token(ctx context.Context) (string, error) {
	itr, err := ghinstallation.NewKeyFromFile(http2.DefaultTransport, g.AppID, g.InstallationID, g.PEMKeyLoc)
	if err != nil {
		return "", fmt.Errorf("unable to generate key from file: %w", err)
	}
	g.Itr = itr
	return itr.Token(ctx)
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
