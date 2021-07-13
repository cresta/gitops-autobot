package ghapp

import (
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	http2 "net/http"
)

type DynamicAuthMethod struct {
	Itr    *ghinstallation.Transport
	Logger *zapctx.Logger
}

const ghAppUserName = "x-access-token"

func (d *DynamicAuthMethod) String() string {
	return fmt.Sprintf("%s - %s:%s", d.Name(), ghAppUserName, "******")
}

func (d *DynamicAuthMethod) Name() string {
	return "dynamic-http-basic-auth"
}

func (d *DynamicAuthMethod) SetAuth(r *http2.Request) {
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

var _ http.AuthMethod = &DynamicAuthMethod{}
