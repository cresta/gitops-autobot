package ghapp

import (
	"context"
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v29/github"
	"go.uber.org/zap"
	http2 "net/http"
	"strings"
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

func EveryPullRequest(ctx context.Context, client *github.Client, repoCfg autobotcfg.RepoConfig) ([]*github.PullRequest, error) {
	owner, repo, err := repoCfg.GithubOwnerRepo()
	if err != nil {
		return nil, fmt.Errorf("unable to parse repository: %w", err)
	}
	var ret []*github.PullRequest
	nextPage := 0
	for {
		prs, resp, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
			Sort: "updated",
			ListOptions: github.ListOptions{
				PerPage: 100,
				Page:    nextPage,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("uanble to list PRs: %w", err)
		}
		ret = append(ret, prs...)
		if resp.NextPage == 0 {
			return ret, nil
		}
		nextPage = resp.NextPage
	}
}

func FetchMasterConfigFile(ctx context.Context, client *github.Client, r autobotcfg.RepoConfig) (*autobotcfg.AutobotPerRepoConfig, error) {
	owner, repo, err := r.GithubOwnerRepo()
	if err != nil {
		return nil, fmt.Errorf("unable to fetch repo owner: %w", err)
	}
	content, _, _, err := client.Repositories.GetContents(ctx, owner, repo, ".gitops-autobot", &github.RepositoryContentGetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to fetch content: %w", err)
	}
	cfgContent, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("unable to decode file content: %w", err)
	}
	ret, err := autobotcfg.LoadPerRepoConfig(strings.NewReader(cfgContent))
	if err != nil {
		return nil, fmt.Errorf("unable to decode repo content: %w", err)
	}
	return ret, nil
}

func AllChecksPass(ctx context.Context, pr *github.PullRequest, logger *zapctx.Logger, client *github.Client) (bool, error) {
	if pr.GetHead().GetRepo().GetOwner().GetLogin() == "" {
		return false, fmt.Errorf("uanble to find owner's name of pr")
	}
	if pr.GetHead().GetRepo().GetName() == "" {
		return false, fmt.Errorf("unable to get the pr's repo")
	}
	if pr.GetNumber() == 0 {
		return false, fmt.Errorf("no pr number set")
	}
	owner, repo := pr.GetHead().GetRepo().GetOwner().GetLogin(), pr.GetHead().GetRepo().GetName()
	checks, _, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, pr.GetHead().GetRef(), &github.ListCheckRunsOptions{
		// TODO: Is it worth listing all pages?
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		return false, fmt.Errorf("unable to list checks for PR: %w", err)
	}
	logger.Debug(ctx, "got checks", zap.Any("checks", checks))
	for _, checkRun := range checks.CheckRuns {
		if checkRun.GetStatus() != "completed" {
			logger.Debug(ctx, "status not completed")
			return false, nil
		}
		allowedStates := []string{"", "neutral", "success", "skipped", "stale"}
		okState := false
		for _, s := range allowedStates {
			if checkRun.GetConclusion() == s {
				okState = true
			}
		}
		if !okState {
			logger.Info(ctx, "check not in ok state", zap.Any("checkrun", checkRun))
			return false, nil
		}
	}
	return true, nil
}
