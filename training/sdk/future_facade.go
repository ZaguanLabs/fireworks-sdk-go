package sdk

import "context"

func (c *FiretitanServiceClient) CreateTrainingClientFuture(ctx context.Context, opts ...CreateFiretitanTrainingClientOptions) *Future[*FiretitanTrainingClient] {
	return SubmitFuture(func() (*FiretitanTrainingClient, error) {
		return c.CreateTrainingClient(ctx, opts...)
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
