package githubdirect

import (
	"context"
	"fmt"
	http2 "net/http"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v29/github"
	"github.com/shurcooL/githubv4"
)

func NewFromConfig(ctx context.Context, cfg autobotcfg.GithubAppConfig, rt http2.RoundTripper, logger *zapctx.Logger) (*GithubDirect, error) {
	trans, err := ghinstallation.NewKeyFromFile(rt, cfg.AppID, cfg.InstallationID, cfg.PEMKeyLoc)
	if err != nil {
		return nil, fmt.Errorf("unable to find key file: %w", err)
	}
	_, err = trans.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to validate token: %w", err)
	}
	gql := githubv4.NewClient(&http2.Client{Transport: trans})
	client := github.NewClient(&http2.Client{Transport: trans})
	return &GithubDirect{
		clientV3:  client,
		clientV4:  gql,
		transport: trans,
		logger:    logger,
	}, nil
}

var _ ghapp.GithubAPI = &GithubDirect{}

type GithubDirect struct {
	clientV3  *github.Client
	clientV4  *githubv4.Client
	transport *ghinstallation.Transport
	logger    *zapctx.Logger
}

func (g *GithubDirect) RepositoryInfo(ctx context.Context, owner string, name string) (*ghapp.RepositoryInfo, error) {
	g.logger.Debug(ctx, "+GithubDirect.RepositoryInfo")
	defer g.logger.Debug(ctx, "-GithubDirect.RepositoryInfo")
	var repoInfo ghapp.RepositoryInfo
	if err := g.clientV4.Query(ctx, &repoInfo, map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}); err != nil {
		return nil, fmt.Errorf("unable to query graphql for repository info: %w", err)
	}
	return &repoInfo, nil
}

func (g *GithubDirect) CreatePullRequest(ctx context.Context, _ string, _ string, in githubv4.CreatePullRequestInput) (*ghapp.CreatePullRequest, error) {
	g.logger.Debug(ctx, "+GithubDirect.CreatePullRequest")
	defer g.logger.Debug(ctx, "-GithubDirect.CreatePullRequest")
	var ret ghapp.CreatePullRequest
	if err := g.clientV4.Mutate(ctx, &ret, in, nil); err != nil {
		return nil, fmt.Errorf("unable to mutate graphql for pull request: %w", err)
	}
	return &ret, nil
}

func (g *GithubDirect) GoGetAuthMethod() http.AuthMethod {
	return &ghapp.DynamicAuthMethod{
		Itr:    g.transport,
		Logger: g.logger,
	}
}

func (g *GithubDirect) GetContents(ctx context.Context, owner string, name string, file string) (string, error) {
	g.logger.Debug(ctx, "+GithubDirect.GetContents")
	defer g.logger.Debug(ctx, "-GithubDirect.GetContents")
	// Note: Cannot find a way to do this with GraphQL
	content, _, _, err := g.clientV3.Repositories.GetContents(ctx, owner, name, file, &github.RepositoryContentGetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to fetch content: %w", err)
	}
	ret, err := content.GetContent()
	if err != nil {
		return "", fmt.Errorf("unable to decode file content: %w", err)
	}
	return ret, nil
}

func (g *GithubDirect) Self(ctx context.Context) (*ghapp.UserInfo, error) {
	g.logger.Debug(ctx, "+GithubDirect.Self")
	defer g.logger.Debug(ctx, "-GithubDirect.Self")
	var q struct {
		Viewer struct {
			ghapp.UserInfo
		}
	}
	if err := g.clientV4.Query(ctx, &q, nil); err != nil {
		return nil, fmt.Errorf("unable to run graphql query: %w", err)
	}
	return &q.Viewer.UserInfo, nil
}

func (g *GithubDirect) AcceptPullRequest(ctx context.Context, _ string, _ string, in githubv4.AddPullRequestReviewInput) (*ghapp.AcceptPullRequestOutput, error) {
	g.logger.Debug(ctx, "+GithubDirect.AcceptPullRequest")
	defer g.logger.Debug(ctx, "-GithubDirect.AcceptPullRequest")
	var ret ghapp.AcceptPullRequestOutput
	if err := g.clientV4.Mutate(ctx, &ret, in, nil); err != nil {
		return nil, fmt.Errorf("unable to graphql accept PR: %w", err)
	}
	return &ret, nil
}

func (g *GithubDirect) MergePullRequest(ctx context.Context, _ string, _ string, _ string, in githubv4.MergePullRequestInput) (*ghapp.MergePullRequestOutput, error) {
	g.logger.Debug(ctx, "+GithubDirect.MergePullRequest")
	defer g.logger.Debug(ctx, "-GithubDirect.MergePullRequest")
	var mergeMutation ghapp.MergePullRequestOutput
	err := g.clientV4.Mutate(ctx, &mergeMutation, in, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to do create a merge: %w", err)
	}
	return &mergeMutation, nil
}

func (g *GithubDirect) EveryOpenPullRequest(ctx context.Context, owner string, name string) (*ghapp.GraphQLPRQuery, error) {
	g.logger.Debug(ctx, "+GithubDirect.EveryOpenPullRequest")
	defer g.logger.Debug(ctx, "-GithubDirect.EveryOpenPullRequest")
	var ret ghapp.GraphQLPRQuery
	if err := g.clientV4.Query(ctx, &ret, map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}); err != nil {
		return nil, fmt.Errorf("unable to query graphql: %w", err)
	}
	return &ret, nil
}

func (g *GithubDirect) DoesBranchExist(ctx context.Context, owner string, name string, ref string) (bool, error) {
	var query struct {
		Repository struct {
			Ref *struct {
				Name githubv4.String
			} `graphql:"ref(qualifiedName: $ref)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}
	if err := g.clientV4.Query(ctx, &query, map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
		"ref":   githubv4.String(ref),
	}); err != nil {
		return false, fmt.Errorf("unable to query graphql: %w", err)
	}
	return query.Repository.Ref != nil, nil
}
