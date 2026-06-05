package fireworks

type ChatMessage struct {
	Role             string                   `json:"role"`
	Content          any                      `json:"content,omitempty"`
	ReasoningContent *string                  `json:"reasoning_content,omitempty"`
	ToolCallID       *string                  `json:"tool_call_id,omitempty"`
	ToolCalls        []ChatCompletionToolCall `json:"tool_calls,omitempty"`
}

type ChatMessageContentPart struct {
	Type     string                          `json:"type"`
	ImageURL *ChatMessageContentPartImageURL `json:"image_url,omitempty"`
	Text     *string                         `json:"text,omitempty"`
	VideoURL *ChatMessageContentPartVideoURL `json:"video_url,omitempty"`
}

type ChatMessageContentPartImageURL struct {
	URL    string  `json:"url"`
	Detail *string `json:"detail,omitempty"`
}

type ChatMessageContentPartVideoURL struct {
	URL          string   `json:"url"`
	Detail       *string  `json:"detail,omitempty"`
	MaxFrames    *int     `json:"max_frames,omitempty"`
	SampleFPS    *float64 `json:"sample_fps,omitempty"`
	SpatialLimit *int     `json:"spatial_limit,omitempty"`
}

type ChatCompletionTool struct {
	Type     string                  `json:"type"`
	Function *ChatCompletionFunction `json:"function,omitempty"`
}

type ChatCompletionFunction struct {
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      *bool          `json:"strict,omitempty"`
}

type ChatCompletionToolCall struct {
	Function any     `json:"function"`
	ID       *string `json:"id,omitempty"`
	Type     *string `json:"type,omitempty"`
}

type ChatCompletionToolCallFunction struct {
	Arguments any     `json:"arguments,omitempty"`
	Name      *string `json:"name,omitempty"`
}

type CompletionCreateParams struct {
	Model                         string             `json:"model"`
	Prompt                        any                `json:"prompt"`
	ContextLengthExceededBehavior *string            `json:"context_length_exceeded_behavior,omitempty"`
	Echo                          *bool              `json:"echo,omitempty"`
	EchoLast                      *int               `json:"echo_last,omitempty"`
	FrequencyPenalty              *float64           `json:"frequency_penalty,omitempty"`
	IgnoreEOS                     *bool              `json:"ignore_eos,omitempty"`
	Images                        any                `json:"images,omitempty"`
	LogitBias                     map[string]float64 `json:"logit_bias,omitempty"`
	Logprobs                      any                `json:"logprobs,omitempty"`
	MaxCompletionTokens           *int               `json:"max_completion_tokens,omitempty"`
	MaxTokens                     *int               `json:"max_tokens,omitempty"`
	Metadata                      map[string]string  `json:"metadata,omitempty"`
	MinP                          *float64           `json:"min_p,omitempty"`
	MirostatLR                    *float64           `json:"mirostat_lr,omitempty"`
	MirostatTarget                *float64           `json:"mirostat_target,omitempty"`
	N                             *int               `json:"n,omitempty"`
	PerfMetricsInResponse         *bool              `json:"perf_metrics_in_response,omitempty"`
	Prediction                    any                `json:"prediction,omitempty"`
	PresencePenalty               *float64           `json:"presence_penalty,omitempty"`
	PromptCacheIsolationKey       *string            `json:"prompt_cache_isolation_key,omitempty"`
	PromptCacheKey                *string            `json:"prompt_cache_key,omitempty"`
	RawOutput                     *bool              `json:"raw_output,omitempty"`
	ReasoningEffort               any                `json:"reasoning_effort,omitempty"`
	ReasoningHistory              *string            `json:"reasoning_history,omitempty"`
	RepetitionPenalty             *float64           `json:"repetition_penalty,omitempty"`
	ResponseFormat                any                `json:"response_format,omitempty"`
	ReturnTokenIDs                *bool              `json:"return_token_ids,omitempty"`
	Seed                          *int               `json:"seed,omitempty"`
	ServiceTier                   *string            `json:"service_tier,omitempty"`
	Speculation                   any                `json:"speculation,omitempty"`
	Stop                          any                `json:"stop,omitempty"`
	Stream                        *bool              `json:"stream,omitempty"`
	Temperature                   *float64           `json:"temperature,omitempty"`
	Thinking                      any                `json:"thinking,omitempty"`
	TopK                          *int               `json:"top_k,omitempty"`
	TopLogprobs                   *int               `json:"top_logprobs,omitempty"`
	TopP                          *float64           `json:"top_p,omitempty"`
	TypicalP                      *float64           `json:"typical_p,omitempty"`
	User                          *string            `json:"user,omitempty"`
}

type ChatCompletionCreateParams struct {
	Messages                      []ChatMessage        `json:"messages"`
	Model                         string               `json:"model"`
	ContextLengthExceededBehavior *string              `json:"context_length_exceeded_behavior,omitempty"`
	Echo                          *bool                `json:"echo,omitempty"`
	EchoLast                      *int                 `json:"echo_last,omitempty"`
	FrequencyPenalty              *float64             `json:"frequency_penalty,omitempty"`
	FunctionCall                  any                  `json:"function_call,omitempty"`
	Functions                     []ChatCompletionTool `json:"functions,omitempty"`
	IgnoreEOS                     *bool                `json:"ignore_eos,omitempty"`
	LogitBias                     map[string]float64   `json:"logit_bias,omitempty"`
	Logprobs                      any                  `json:"logprobs,omitempty"`
	MaxCompletionTokens           *int                 `json:"max_completion_tokens,omitempty"`
	MaxTokens                     *int                 `json:"max_tokens,omitempty"`
	Metadata                      map[string]string    `json:"metadata,omitempty"`
	MinP                          *float64             `json:"min_p,omitempty"`
	MirostatLR                    *float64             `json:"mirostat_lr,omitempty"`
	MirostatTarget                *float64             `json:"mirostat_target,omitempty"`
	N                             *int                 `json:"n,omitempty"`
	ParallelToolCalls             *bool                `json:"parallel_tool_calls,omitempty"`
	PerfMetricsInResponse         *bool                `json:"perf_metrics_in_response,omitempty"`
	Prediction                    any                  `json:"prediction,omitempty"`
	PresencePenalty               *float64             `json:"presence_penalty,omitempty"`
	PromptCacheIsolationKey       *string              `json:"prompt_cache_isolation_key,omitempty"`
	PromptCacheKey                *string              `json:"prompt_cache_key,omitempty"`
	PromptTruncateLen             *int                 `json:"prompt_truncate_len,omitempty"`
	RawOutput                     *bool                `json:"raw_output,omitempty"`
	ReasoningEffort               any                  `json:"reasoning_effort,omitempty"`
	ReasoningHistory              *string              `json:"reasoning_history,omitempty"`
	RepetitionPenalty             *float64             `json:"repetition_penalty,omitempty"`
	ResponseFormat                any                  `json:"response_format,omitempty"`
	ReturnTokenIDs                *bool                `json:"return_token_ids,omitempty"`
	SafeTokenization              *bool                `json:"safe_tokenization,omitempty"`
	Seed                          *int                 `json:"seed,omitempty"`
	ServiceTier                   *string              `json:"service_tier,omitempty"`
	Speculation                   any                  `json:"speculation,omitempty"`
	Stop                          any                  `json:"stop,omitempty"`
	Stream                        *bool                `json:"stream,omitempty"`
	Temperature                   *float64             `json:"temperature,omitempty"`
	Thinking                      any                  `json:"thinking,omitempty"`
	ToolChoice                    any                  `json:"tool_choice,omitempty"`
	Tools                         []ChatCompletionTool `json:"tools,omitempty"`
	TopK                          *int                 `json:"top_k,omitempty"`
	TopLogprobs                   *int                 `json:"top_logprobs,omitempty"`
	TopP                          *float64             `json:"top_p,omitempty"`
	TypicalP                      *float64             `json:"typical_p,omitempty"`
	User                          *string              `json:"user,omitempty"`
}

type UsageInfo struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	CompletionTokens    *int                 `json:"completion_tokens,omitempty"`
	PromptTokensDetails *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens *int `json:"cached_tokens,omitempty"`
}

type CompletionCreateResponse struct {
	ID          string             `json:"id"`
	Choices     []CompletionChoice `json:"choices"`
	Created     int64              `json:"created"`
	Model       string             `json:"model"`
	Usage       UsageInfo          `json:"usage"`
	Object      *string            `json:"object,omitempty"`
	PerfMetrics map[string]any     `json:"perf_metrics,omitempty"`
}

type CompletionChoice struct {
	Index          int        `json:"index"`
	Text           string     `json:"text"`
	FinishReason   *string    `json:"finish_reason,omitempty"`
	Logprobs       any        `json:"logprobs,omitempty"`
	PromptTokenIDs []int      `json:"prompt_token_ids,omitempty"`
	RawOutput      *RawOutput `json:"raw_output,omitempty"`
	TokenIDs       []int      `json:"token_ids,omitempty"`
}

type CompletionChunk struct {
	ID          string             `json:"id"`
	Choices     []CompletionChoice `json:"choices"`
	Created     int64              `json:"created"`
	Model       string             `json:"model"`
	Object      *string            `json:"object,omitempty"`
	PerfMetrics map[string]any     `json:"perf_metrics,omitempty"`
	Usage       *UsageInfo         `json:"usage,omitempty"`
}

type ChatCompletionCreateResponse struct {
	ID             string                 `json:"id"`
	Choices        []ChatCompletionChoice `json:"choices"`
	Created        int64                  `json:"created"`
	Model          string                 `json:"model"`
	Object         *string                `json:"object,omitempty"`
	PerfMetrics    map[string]any         `json:"perf_metrics,omitempty"`
	PromptTokenIDs []int                  `json:"prompt_token_ids,omitempty"`
	Usage          *UsageInfo             `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason *string     `json:"finish_reason,omitempty"`
	Logprobs     any         `json:"logprobs,omitempty"`
	RawOutput    *RawOutput  `json:"raw_output,omitempty"`
	TokenIDs     []int       `json:"token_ids,omitempty"`
}

type ChatCompletionChunk struct {
	ID             string                      `json:"id"`
	Choices        []ChatCompletionChunkChoice `json:"choices"`
	Created        int64                       `json:"created"`
	Model          string                      `json:"model"`
	Object         *string                     `json:"object,omitempty"`
	PerfMetrics    map[string]any              `json:"perf_metrics,omitempty"`
	PromptTokenIDs []int                       `json:"prompt_token_ids,omitempty"`
	Usage          *UsageInfo                  `json:"usage,omitempty"`
}

type ChatCompletionChunkChoice struct {
	Delta          ChatCompletionChunkDelta `json:"delta"`
	Index          int                      `json:"index"`
	FinishReason   *string                  `json:"finish_reason,omitempty"`
	Logprobs       any                      `json:"logprobs,omitempty"`
	PromptTokenIDs []int                    `json:"prompt_token_ids,omitempty"`
	RawOutput      *RawOutput               `json:"raw_output,omitempty"`
	TokenIDs       []int                    `json:"token_ids,omitempty"`
}

type ChatCompletionChunkDelta struct {
	Content          *string                  `json:"content,omitempty"`
	ReasoningContent *string                  `json:"reasoning_content,omitempty"`
	Role             *string                  `json:"role,omitempty"`
	ToolCalls        []ChatCompletionToolCall `json:"tool_calls,omitempty"`
}

type RawOutput struct {
	Completion         string       `json:"completion"`
	PromptFragments    []any        `json:"prompt_fragments"`
	PromptTokenIDs     []int        `json:"prompt_token_ids"`
	CompletionLogprobs *NewLogProbs `json:"completion_logprobs,omitempty"`
	CompletionTokenIDs []int        `json:"completion_token_ids,omitempty"`
	Grammar            *string      `json:"grammar,omitempty"`
	Images             []string     `json:"images,omitempty"`
	Videos             []string     `json:"videos,omitempty"`
}

type LogProbs struct {
	TextOffset    []int                `json:"text_offset,omitempty"`
	TokenIDs      []int                `json:"token_ids,omitempty"`
	TokenLogprobs []float64            `json:"token_logprobs,omitempty"`
	Tokens        []string             `json:"tokens,omitempty"`
	TopLogprobs   []map[string]float64 `json:"top_logprobs,omitempty"`
}

type NewLogProbs struct {
	Content []NewLogProbsContent `json:"content,omitempty"`
}

type NewLogProbsContent struct {
	Token       string                  `json:"token"`
	Logprob     float64                 `json:"logprob"`
	Bytes       []int                   `json:"bytes,omitempty"`
	TopLogprobs []NewLogProbsTopLogprob `json:"top_logprobs,omitempty"`
}

type NewLogProbsTopLogprob struct {
	Token   string  `json:"token"`
	Logprob float64 `json:"logprob"`
	Bytes   []int   `json:"bytes,omitempty"`
}
