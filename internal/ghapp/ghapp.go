package ghapp

import (
	"context"
	"fmt"
	"net/http"
)
import "github.com/bradleyfalzon/ghinstallation"

type GhApp struct {
	AppID int64
	InstallationID int64
	PEMKeyLoc string
	Itr *ghinstallation.Transport
}

func (g *GhApp) Token(ctx context.Context) (string, error) {
	itr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, g.AppID, g.InstallationID, g.PEMKeyLoc)
	if err != nil {
		return "", fmt.Errorf("unable to generate key from file: %w", err)
	}
	g.Itr = itr
	return itr.Token(ctx)
}