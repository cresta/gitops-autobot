package cachedgithub

import (
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/cache"
	"github.com/cresta/gitops-autobot/internal/ghapp"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/shurcooL/githubv4"
	"sync"
	"time"
)

type CachedGithub struct {
	Into  ghapp.GithubAPI
	Cache cache.Cache
	mu    sync.Mutex
	self  *ghapp.UserInfo
}

const cacheVersion = "1"

func (c *CachedGithub) generalKey(function string, owner string, name string, extraInfo string) []byte {
	return []byte(fmt.Sprintf("%s:%s:%s:%s:%s", cacheVersion, function, owner, name, extraInfo))
}

func (c *CachedGithub) listPrsKey(owner string, name string) []byte {
	return c.generalKey("listPrs", owner, name, "")
}

func (c *CachedGithub) RepositoryInfo(ctx context.Context, owner string, name string) (*ghapp.RepositoryInfo, error) {
	var ret ghapp.RepositoryInfo
	if err := c.Cache.GetOrSet(ctx, c.generalKey("repoInfo", owner, name, ""), time.Hour, &ret, func(ctx context.Context) (interface{}, error) {
		return c.Into.RepositoryInfo(ctx, owner, name)
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch from cache: %w", err)
	}
	return &ret, nil
}

func (c *CachedGithub) GetContents(ctx context.Context, owner string, name string, file string) (string, error) {
	var ret string
	if err := c.Cache.GetOrSet(ctx, c.generalKey("getCont", owner, name, file), time.Minute*5, &ret, func(ctx context.Context) (interface{}, error) {
		return c.Into.GetContents(ctx, owner, name, file)
	}); err != nil {
		return "", fmt.Errorf("unable to fetch from cache: %w", err)
	}
	return ret, nil
}

func (c *CachedGithub) GoGetAuthMethod() http.AuthMethod {
	return c.Into.GoGetAuthMethod()
}

func (c *CachedGithub) Self(ctx context.Context) (*ghapp.UserInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.self != nil {
		return c.self, nil
	}
	newSelf, err := c.Into.Self(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch self: %w", err)
	}
	c.self = newSelf
	return c.self, nil
}

func (c *CachedGithub) EveryOpenPullRequest(ctx context.Context, owner string, name string) (*ghapp.GraphQLPRQuery, error) {
	var ret ghapp.GraphQLPRQuery
	if err := c.Cache.GetOrSet(ctx, c.listPrsKey(owner, name), time.Hour, &ret, func(ctx context.Context) (interface{}, error) {
		return c.Into.EveryOpenPullRequest(ctx, owner, name)
	}); err != nil {
		return nil, fmt.Errorf("unable to fetch from cache: %w", err)
	}
	return &ret, nil
}

func (c *CachedGithub) AcceptPullRequest(ctx context.Context, owner string, name string, in githubv4.AddPullRequestReviewInput) (*ghapp.AcceptPullRequestOutput, error) {
	if err := c.Cache.Delete(ctx, c.listPrsKey(owner, name)); err != nil {
		return nil, fmt.Errorf("unable to clear out cache: %w", err)
	}
	return c.Into.AcceptPullRequest(ctx, owner, name, in)
}

func (c *CachedGithub) MergePullRequest(ctx context.Context, owner string, name string, in githubv4.MergePullRequestInput) (*ghapp.MergePullRequestOutput, error) {
	if err := c.Cache.Delete(ctx, c.listPrsKey(owner, name)); err != nil {
		return nil, fmt.Errorf("unable to clear out cache: %w", err)
	}
	return c.Into.MergePullRequest(ctx, owner, name, in)
}

func (c *CachedGithub) CreatePullRequest(ctx context.Context, owner string, name string, in githubv4.CreatePullRequestInput) (*ghapp.CreatePullRequest, error) {
	if err := c.Cache.Delete(ctx, c.listPrsKey(owner, name)); err != nil {
		return nil, fmt.Errorf("unable to clear out cache: %w", err)
	}
	return c.Into.CreatePullRequest(ctx, owner, name, in)
}

var _ ghapp.GithubAPI = &CachedGithub{}
