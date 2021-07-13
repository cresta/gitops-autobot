package ghapp

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/autobotcfg"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/shurcooL/githubv4"
	"strings"
)

type GithubAPI interface {
	RepositoryInfo(ctx context.Context, owner string, name string) (*RepositoryInfo, error)
	CreatePullRequest(ctx context.Context, owner string, name string, in githubv4.CreatePullRequestInput) (*CreatePullRequest, error)
	GoGetAuthMethod() http.AuthMethod
	GetContents(ctx context.Context, owner string, name string, file string) (string, error)
	Self(ctx context.Context) (*UserInfo, error)
	AcceptPullRequest(ctx context.Context, owner string, name string, in githubv4.AddPullRequestReviewInput) (*AcceptPullRequestOutput, error)
	MergePullRequest(ctx context.Context, owner string, name string, in githubv4.MergePullRequestInput) (*MergePullRequestOutput, error)
	EveryOpenPullRequest(ctx context.Context, owner string, name string) (*GraphQLPRQuery, error)
}

type RepositoryInfo struct {
	Repository struct {
		Id               githubv4.ID
		DefaultBranchRef struct {
			Name githubv4.String
			ID   githubv4.ID
		}
	} `graphql:"repository(owner: $owner, name: $name)"`
}

type CreatePullRequest struct {
	CreatePullRequest struct {
		// Note: This is unused, but the library requires at least something to be read for the mutation to happen
		ClientMutationId githubv4.ID
	} `graphql:"createPullRequest(input: $input)"`
}

type GraphQLPRQueryNode struct {
	Id                githubv4.ID
	Number            githubv4.Int
	Locked            githubv4.Boolean
	Merged            githubv4.Boolean
	IsDraft           githubv4.Boolean
	Mergeable         githubv4.MergeableState
	State             githubv4.PullRequestState
	Body              githubv4.String
	UpdatedAt         githubv4.DateTime
	ReviewDecision    githubv4.PullRequestReviewDecision
	IsCrossRepository githubv4.Boolean
	Repository        struct {
		Owner struct {
			Login githubv4.String
		}
		Name githubv4.String
	}
	Author struct {
		Login githubv4.String
		Bot   struct {
			Id githubv4.ID
		} `graphql:"... on Bot"`
		User struct {
			Id githubv4.ID
		} `graphql:"... on User"`
	}
	ViewerLatestReview struct {
		State  githubv4.String
		Commit struct {
			Oid githubv4.GitObjectID
		}
		AuthorCanPushToRepository githubv4.Boolean
	}
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
		} `graphql:"pullRequests(first: 100, states: [OPEN])"`
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

type AcceptPullRequestOutput struct {
	AddPullRequestReview struct {
		PullRequestReview struct {
			Id githubv4.ID
		}
	} `graphql:"addPullRequestReview(input: $input)"`
}

func PopulateRepoDefaultBranches(ctx context.Context, cfg *autobotcfg.AutobotConfig, g GithubAPI) (*autobotcfg.AutobotConfig, error) {
	for idx := range cfg.Repos {
		if cfg.Repos[idx].Branch != "" {
			continue
		}
		ri, err := g.RepositoryInfo(ctx, cfg.Repos[idx].Owner, cfg.Repos[idx].Name)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch repo info: %w", err)
		}
		if ri.Repository.DefaultBranchRef.Name == "" {
			return cfg, fmt.Errorf("unable to load default branch name from graphql")
		}
		cfg.Repos[idx].Branch = string(ri.Repository.DefaultBranchRef.Name)
	}
	return cfg, nil
}

func FetchMasterConfigFile(ctx context.Context, g GithubAPI, r autobotcfg.RepoConfig) (*autobotcfg.AutobotPerRepoConfig, error) {
	// Note: Cannot find a way to do this with GraphQL
	content, err := g.GetContents(ctx, r.Owner, r.Name, ".gitops-autobot")
	if err != nil {
		return nil, fmt.Errorf("unable to fetch contents: %w", err)
	}
	ret, err := autobotcfg.LoadPerRepoConfig(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("unable to decode repo content: %w", err)
	}
	return ret, nil
}
