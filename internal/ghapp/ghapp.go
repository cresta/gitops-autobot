package ghapp

import (
	"context"
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/cresta/zapctx"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v29/github"
	"github.com/shurcooL/githubv4"
	http2 "net/http"
	"strings"
)

func NewFromConfig(ctx context.Context, cfg autobotcfg.GithubAppConfig, rt http2.RoundTripper, logger *zapctx.Logger) (*GithubAPI, error) {
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
	return &GithubAPI{
		clientV3:  client,
		clientV4:  gql,
		transport: trans,
		logger:    logger,
	}, nil
}

type GithubAPI struct {
	clientV3  *github.Client
	clientV4  *githubv4.Client
	transport *ghinstallation.Transport
	logger    *zapctx.Logger
}

type RepositoryInfo struct {
	Repository struct {
		Id githubv4.ID
	} `graphql:"repository(owner: $owner, name: $name)"`
}

func (g *GithubAPI) RepositoryInfo(ctx context.Context, owner string, name string) (*RepositoryInfo, error) {
	var repoInfo RepositoryInfo
	if err := g.clientV4.Query(ctx, &repoInfo, map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}); err != nil {
		return nil, fmt.Errorf("unable to query graphql for repository info: %w", err)
	}
	return &repoInfo, nil
}

type CreatePullRequest struct {
	CreatePullRequest struct {
		// Note: This is unused, but the library requires at least something to be read for the mutation to happen
		ClientMutationId githubv4.ID
	} `graphql:"createPullRequest(input: $input)"`
}

func (g *GithubAPI) CreatePullRequest(ctx context.Context, in *githubv4.CreatePullRequestInput) (*CreatePullRequest, error) {
	var ret CreatePullRequest
	if err := g.clientV4.Mutate(ctx, &ret, in, nil); err != nil {
		return nil, fmt.Errorf("unable to mutate graphql for pull request: %w", err)
	}
	return &ret, nil
}

func (g *GithubAPI) GoGetAuthMethod() http.AuthMethod {
	return &dynamicAuthMethod{
		Itr:    g.transport,
		Logger: g.logger,
	}
}

func (g *GithubAPI) FetchMasterConfigFile(ctx context.Context, r autobotcfg.RepoConfig) (*autobotcfg.AutobotPerRepoConfig, error) {
	// Note: Cannot find a way to do this with GraphQL
	owner, repo, err := r.GithubOwnerRepo()
	if err != nil {
		return nil, fmt.Errorf("unable to fetch repo owner: %w", err)
	}
	content, _, _, err := g.clientV3.Repositories.GetContents(ctx, owner, repo, ".gitops-autobot", &github.RepositoryContentGetOptions{})
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

type dynamicAuthMethod struct {
	Itr    *ghinstallation.Transport
	Logger *zapctx.Logger
}

const ghAppUserName = "x-access-token"

func (d *dynamicAuthMethod) String() string {
	return fmt.Sprintf("%s - %s:%s", d.Name(), ghAppUserName, "******")
}

func (d *dynamicAuthMethod) Name() string {
	return "dynamic-http-basic-auth"
}

func (d *dynamicAuthMethod) SetAuth(r *http2.Request) {
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

var _ http.AuthMethod = &dynamicAuthMethod{}

type GraphQLPRQueryNode struct {
	Id         githubv4.ID
	Number     githubv4.Int
	Locked     githubv4.Boolean
	Merged     githubv4.Boolean
	IsDraft    githubv4.Boolean
	Mergeable  githubv4.MergeableState
	State      githubv4.PullRequestState
	Body       githubv4.String
	UpdatedAt  githubv4.DateTime
	Repository struct {
		Owner struct {
			Login githubv4.String
		}
		Name githubv4.String
	}
	Author struct {
		Login githubv4.String
		Bot   struct {
			Id githubv4.String
		} `graphql:"... on Bot"`
		User struct {
			Id githubv4.String
		} `graphql:"... on User"`
	}
	Reviews struct {
		Nodes []struct {
			State  githubv4.String
			Commit struct {
				Oid githubv4.GitObjectID
			}
			AuthorCanPushToRepository githubv4.Boolean
			Author                    struct {
				Login githubv4.String
				Bot   struct {
					Id githubv4.ID
				} `graphql:"... on Bot"`
				User struct {
					Id githubv4.ID
				} `graphql:"... on User"`
			}
		}
	} `graphql:"reviews(first: 100, after: $reviews_after, states: [APPROVED], author: $pr_author)"`
	HeadRef struct {
		Target struct {
			Oid    githubv4.GitObjectID
			Commit struct {
				StatusCheckRollup struct {
					State githubv4.StatusState
				}
			} `graphql:"... on Commit"`
		}
	}
}

type GraphQLPRQuery struct {
	Repository struct {
		PullRequests struct {
			Nodes []GraphQLPRQueryNode
		} `graphql:"pullRequests(first: 100, after: $pr_after, states: $states)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

type MergePullRequestOutput struct {
	MergePullRequest struct {
		PullRequest struct {
			Id githubv4.ID
		}
	} `graphql:"mergePullRequest(input: $input)"`
}

type UserInfo struct {
	Login githubv4.String
	Id    githubv4.ID
}

func (g *GithubAPI) Self(ctx context.Context) (*UserInfo, error) {
	var q struct {
		Viewer struct {
			UserInfo
		}
	}
	if err := g.clientV4.Query(ctx, &q, nil); err != nil {
		return nil, fmt.Errorf("unable to run graphql query: %w", err)
	}
	return &q.Viewer.UserInfo, nil
}

type AcceptPullRequestOutput struct {
	AddPullRequestReview struct {
		PullRequestReview struct {
			Id githubv4.ID
		}
	} `graphql:"addPullRequestReview(input: $input)"`
}

func (g *GithubAPI) AcceptPullRequest(ctx context.Context, in githubv4.AddPullRequestReviewInput) (*AcceptPullRequestOutput, error) {
	var ret AcceptPullRequestOutput
	if err := g.clientV4.Mutate(ctx, &ret, in, nil); err != nil {
		return nil, fmt.Errorf("unable to graphql accept PR: %w", err)
	}
	return &ret, nil
}

func (g *GithubAPI) MergePullRequest(ctx context.Context, in githubv4.MergePullRequestInput) (*MergePullRequestOutput, error) {
	var mergeMutation MergePullRequestOutput
	err := g.clientV4.Mutate(ctx, &mergeMutation, in, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to do create a merge: %w", err)
	}
	return &mergeMutation, nil
}

func (g *GithubAPI) EveryPullRequest(ctx context.Context, repoCfg autobotcfg.RepoConfig) (*GraphQLPRQuery, error) {
	owner, repo, err := repoCfg.GithubOwnerRepo()
	if err != nil {
		return nil, fmt.Errorf("unable to parse repository: %w", err)
	}
	var ret GraphQLPRQuery
	err = g.clientV4.Query(ctx, &ret, map[string]interface{}{
		"owner":         githubv4.String(owner),
		"name":          githubv4.String(repo),
		"pr_after":      (*githubv4.String)(nil),
		"reviews_after": (*githubv4.String)(nil),
		"pr_author":     (*githubv4.String)(nil),
		"states":        []githubv4.PullRequestState{githubv4.PullRequestStateOpen},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to query graphql: %w", err)
	}
	return &ret, nil
}
