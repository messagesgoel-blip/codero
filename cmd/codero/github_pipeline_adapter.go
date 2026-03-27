package main

import (
	"context"

	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
	ghclient "github.com/codero/codero/internal/github"
)

// githubPipelineAdapter wraps *github.Client to implement deliverypipeline.GitHubClient.
type githubPipelineAdapter struct {
	client *ghclient.Client
}

func (a *githubPipelineAdapter) CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (int, bool, error) {
	return a.client.CreatePRIfEnabled(ctx, repo, head, base, title, body)
}

func (a *githubPipelineAdapter) TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error {
	return a.client.TriggerCodeRabbitReview(ctx, repo, prNumber)
}

func (a *githubPipelineAdapter) FindOpenPR(ctx context.Context, repo, branch string) (*deliverypipeline.PRInfo, error) {
	pr, err := a.client.FindOpenPR(ctx, repo, branch)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, nil
	}
	return &deliverypipeline.PRInfo{
		Number:  pr.Number,
		HeadSHA: pr.HeadSHA,
	}, nil
}

func (a *githubPipelineAdapter) ListCheckRuns(ctx context.Context, repo, sha string) ([]deliverypipeline.CheckRunInfo, error) {
	runs, err := a.client.ListCheckRuns(ctx, repo, sha)
	if err != nil {
		return nil, err
	}
	result := make([]deliverypipeline.CheckRunInfo, len(runs))
	for i, r := range runs {
		result[i] = deliverypipeline.CheckRunInfo{
			Name:       r.Name,
			Status:     r.Status,
			Conclusion: r.Conclusion,
		}
	}
	return result, nil
}

func (a *githubPipelineAdapter) ListPRReviews(ctx context.Context, repo string, prNumber int) ([]deliverypipeline.ReviewInfo, error) {
	reviews, err := a.client.ListPRReviews(ctx, repo, prNumber)
	if err != nil {
		return nil, err
	}
	result := make([]deliverypipeline.ReviewInfo, len(reviews))
	for i, r := range reviews {
		result[i] = deliverypipeline.ReviewInfo{
			State: r.State,
			User:  r.User,
			IsBot: ghclient.IsBot(r.User),
		}
	}
	return result, nil
}

func (a *githubPipelineAdapter) MergePR(ctx context.Context, repo string, prNumber int, sha, mergeMethod string) error {
	return a.client.MergePR(ctx, repo, prNumber, sha, mergeMethod)
}
