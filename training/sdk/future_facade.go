package sdk

import "context"

func (c *FiretitanServiceClient) CreateTrainingClientFuture(ctx context.Context, opts ...CreateFiretitanTrainingClientOptions) *Future[*FiretitanTrainingClient] {
	return SubmitFuture(func() (*FiretitanTrainingClient, error) {
		return c.CreateTrainingClient(ctx, opts...)
	})
}

func (c *FiretitanServiceClient) CreateBaseTrainingClientFuture(ctx context.Context, baseModel string, userMetadata map[string]string) *Future[*FiretitanTrainingClient] {
	return SubmitFuture(func() (*FiretitanTrainingClient, error) {
		return c.CreateBaseTrainingClient(ctx, baseModel, userMetadata)
	})
}

func (c *FiretitanServiceClient) CreateReferenceClientFuture(ctx context.Context, baseModel string, opts ...CreateReferenceClientOptions) *Future[*FiretitanTrainingClient] {
	return SubmitFuture(func() (*FiretitanTrainingClient, error) {
		return c.CreateReferenceClient(ctx, baseModel, opts...)
	})
}

func (c *FiretitanServiceClient) CreateTrainingClientFromStateFuture(ctx context.Context, path string, opts ...CreateTrainingClientFromStateOptions) *Future[*FiretitanTrainingClient] {
	return SubmitFuture(func() (*FiretitanTrainingClient, error) {
		return c.CreateTrainingClientFromState(ctx, path, opts...)
	})
}

func (c *FiretitanServiceClient) CreateTrainingClientFromStateWithOptimizerFuture(ctx context.Context, path string, opts ...CreateTrainingClientFromStateOptions) *Future[*FiretitanTrainingClient] {
	return SubmitFuture(func() (*FiretitanTrainingClient, error) {
		return c.CreateTrainingClientFromStateWithOptimizer(ctx, path, opts...)
	})
}

func (c *FiretitanTrainingClient) SaveWeightsForSamplerFuture(ctx context.Context, name string, checkpointType ...string) *Future[SaveSamplerResult] {
	return SubmitFuture(func() (SaveSamplerResult, error) {
		return c.SaveWeightsForSampler(ctx, name, checkpointType...)
	})
}

func (c *FiretitanTrainingClient) SaveWeightsAndHotloadFuture(ctx context.Context, name string, checkpointType ...string) *Future[SaveSamplerResult] {
	return SubmitFuture(func() (SaveSamplerResult, error) {
		return c.SaveWeightsAndHotload(ctx, name, checkpointType...)
	})
}

func (c *FiretitanTrainingClient) CreateSamplingClientFuture(ctx context.Context, modelPath string, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController) *Future[*FiretitanSamplingClient] {
	return SubmitFuture(func() (*FiretitanSamplingClient, error) {
		return c.CreateSamplingClient(ctx, modelPath, tokenizer, controller)
	})
}

func (c *FiretitanTrainingClient) SaveWeightsAndGetSamplingClientFuture(ctx context.Context, name string, tokenizer DeploymentTokenizer, checkpointType ...string) *Future[*FiretitanSamplingClient] {
	return SubmitFuture(func() (*FiretitanSamplingClient, error) {
		return c.SaveWeightsAndGetSamplingClient(ctx, name, tokenizer, checkpointType...)
	})
}

func (c *FiretitanTrainingClient) SaveStateFuture(ctx context.Context, name string, opts ...SaveStateOptions) *Future[SaveStateResult] {
	return SubmitFuture(func() (SaveStateResult, error) {
		return c.SaveState(ctx, name, opts...)
	})
}

func (c *FiretitanTrainingClient) LoadStateFuture(ctx context.Context, path string, weightsAccessToken *string) *Future[struct{}] {
	return SubmitFuture(func() (struct{}, error) {
		return struct{}{}, c.LoadState(ctx, path, weightsAccessToken)
	})
}

func (c *FiretitanTrainingClient) LoadStateWithOptimizerFuture(ctx context.Context, path string, weightsAccessToken *string) *Future[struct{}] {
	return SubmitFuture(func() (struct{}, error) {
		return struct{}{}, c.LoadStateWithOptimizer(ctx, path, weightsAccessToken)
	})
}

func (c *FiretitanTrainingClient) LoadAdapterFuture(ctx context.Context, adapterPath string) *Future[LoadAdapterResponse] {
	return SubmitFuture(func() (LoadAdapterResponse, error) {
		return c.LoadAdapter(ctx, adapterPath)
	})
}

func (c *FiretitanSamplingClient) SampleFuture(ctx context.Context, prompt []int, numSamples int, params FiretitanSamplingParams, opts ...FiretitanSampleOptions) *Future[FiretitanSampleResponse] {
	return SubmitFuture(func() (FiretitanSampleResponse, error) {
		return c.Sample(ctx, prompt, numSamples, params, opts...)
	})
}

func (c *FiretitanSamplingClient) ComputeLogprobsFuture(ctx context.Context, prompt []int) *Future[[]*float64] {
	return SubmitFuture(func() ([]*float64, error) {
		return c.ComputeLogprobs(ctx, prompt)
	})
}

func (c *FiretitanSamplingClient) GetBaseModelFuture(context.Context) *Future[string] {
	return ReadyFuture(c.GetBaseModel(), nil)
}
