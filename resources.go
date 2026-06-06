package fireworks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

type resource struct {
	client *Client
}

func (r resource) get(ctx context.Context, path string, opts ...RequestOption) (Response, error) {
	var out Response
	err := r.client.Request(ctx, http.MethodGet, path, nil, &out, opts...)
	return out, err
}

func (r resource) post(ctx context.Context, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := r.client.Request(ctx, http.MethodPost, path, body, &out, opts...)
	return out, err
}

func (r resource) patch(ctx context.Context, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := r.client.Request(ctx, http.MethodPatch, path, body, &out, opts...)
	return out, err
}

func (r resource) delete(ctx context.Context, path string, opts ...RequestOption) (Response, error) {
	var out Response
	err := r.client.Request(ctx, http.MethodDelete, path, nil, &out, opts...)
	return out, err
}

func (r resource) accountID(opts []RequestOption) (string, error) {
	return r.client.resolveAccountID(applyRequestOptions(opts))
}

func (r resource) accountPath(opts []RequestOption, makePath func(accountID string) string) (string, error) {
	accountID, err := r.accountID(opts)
	if err != nil {
		return "", err
	}
	return r.client.managementPath(makePath(pathEscape(accountID))), nil
}

func withQuery(query any, opts []RequestOption) []RequestOption {
	values, accountID, ok := queryMap(query)
	if !ok && accountID == "" {
		return opts
	}
	out := make([]RequestOption, 0, len(opts)+2)
	if accountID != "" {
		out = append(out, WithAccountID(accountID))
	}
	if ok {
		out = append(out, WithQuery(values))
	}
	out = append(out, opts...)
	return out
}

func queryMap(query any) (map[string]any, string, bool) {
	switch v := query.(type) {
	case nil:
		return nil, "", false
	case map[string]any:
		out, accountID := splitAccountID(v)
		out = normalizeQueryMap(out)
		return out, accountID, len(out) > 0
	case url.Values:
		out := make(map[string]any, len(v))
		for key, values := range v {
			switch len(values) {
			case 0:
				continue
			case 1:
				out[key] = values[0]
			default:
				out[key] = values
			}
		}
		out, accountID := splitAccountID(out)
		return out, accountID, len(out) > 0
	default:
		payload, err := json.Marshal(query)
		if err != nil || string(payload) == "null" || string(payload) == "{}" {
			return nil, "", false
		}
		var out map[string]any
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil, "", false
		}
		out, accountID := splitAccountID(out)
		out = normalizeQueryMap(out)
		return out, accountID, len(out) > 0
	}
}

func normalizeQueryMap(query map[string]any) map[string]any {
	out := make(map[string]any, len(query))
	for key, value := range query {
		if value == nil || key == "account_id" {
			continue
		}
		out[queryAlias(key)] = value
	}
	return out
}

func splitAccountID(query map[string]any) (map[string]any, string) {
	out := make(map[string]any, len(query))
	var accountID string
	for key, value := range query {
		if key == "account_id" {
			if raw, ok := value.(string); ok {
				accountID = raw
			}
			continue
		}
		out[key] = value
	}
	return out, accountID
}

func queryAlias(key string) string {
	switch key {
	case "order_by":
		return "orderBy"
	case "page_size":
		return "pageSize"
	case "page_token":
		return "pageToken"
	case "read_mask":
		return "readMask"
	case "config_only":
		return "configOnly"
	case "skip_hf_config_validation":
		return "skipHfConfigValidation"
	case "trust_remote_code":
		return "trustRemoteCode"
	default:
		return managementBodyAlias(key)
	}
}

func pathEscape(value string) string {
	return url.PathEscape(value)
}

type ChatResource struct {
	client      *Client
	Completions *ChatCompletionsResource
}

type ChatCompletionsResource struct {
	client *Client
}

func (r *ChatCompletionsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return resource{r.client}.post(ctx, r.client.inferencePath("/v1/chat/completions"), body, opts...)
}

func (r *ChatCompletionsResource) CreateTyped(ctx context.Context, params fwtypes.ChatCompletionCreateParams, opts ...RequestOption) (*fwtypes.ChatCompletionCreateResponse, error) {
	var out fwtypes.ChatCompletionCreateResponse
	err := r.client.Request(ctx, http.MethodPost, r.client.inferencePath("/v1/chat/completions"), params, &out, opts...)
	return &out, err
}

func (r *ChatCompletionsResource) CreateStream(ctx context.Context, body any, opts ...RequestOption) (*Stream, error) {
	return newStream(ctx, r.client, r.client.inferencePath("/v1/chat/completions"), body, opts...)
}

func (r *ChatCompletionsResource) CreateTypedStream(ctx context.Context, params fwtypes.ChatCompletionCreateParams, opts ...RequestOption) (*TypedStream[fwtypes.ChatChatCompletionChunk], error) {
	return newTypedStream[fwtypes.ChatChatCompletionChunk](ctx, r.client, r.client.inferencePath("/v1/chat/completions"), params, opts...)
}

type CompletionsResource struct {
	client *Client
}

func (r *CompletionsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return resource{r.client}.post(ctx, r.client.inferencePath("/v1/completions"), body, opts...)
}

func (r *CompletionsResource) CreateTyped(ctx context.Context, params fwtypes.CompletionCreateParams, opts ...RequestOption) (*fwtypes.CompletionCreateResponse, error) {
	var out fwtypes.CompletionCreateResponse
	err := r.client.Request(ctx, http.MethodPost, r.client.inferencePath("/v1/completions"), params, &out, opts...)
	return &out, err
}

func (r *CompletionsResource) CreateStream(ctx context.Context, body any, opts ...RequestOption) (*Stream, error) {
	return newStream(ctx, r.client, r.client.inferencePath("/v1/completions"), body, opts...)
}

func (r *CompletionsResource) CreateTypedStream(ctx context.Context, params fwtypes.CompletionCreateParams, opts ...RequestOption) (*TypedStream[fwtypes.CompletionChunk], error) {
	return newTypedStream[fwtypes.CompletionChunk](ctx, r.client, r.client.inferencePath("/v1/completions"), params, opts...)
}

type MessagesResource struct {
	client *Client
}

func (r *MessagesResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return resource{r.client}.post(ctx, r.client.inferencePath("/v1/messages"), body, opts...)
}

func (r *MessagesResource) CreateTyped(ctx context.Context, params fwtypes.MessageCreateParams, opts ...RequestOption) (*fwtypes.MessageCreateResponse, error) {
	var out fwtypes.MessageCreateResponse
	err := r.client.Request(ctx, http.MethodPost, r.client.inferencePath("/v1/messages"), params, &out, opts...)
	return &out, err
}

type AccountsResource struct {
	client *Client
}

func (r *AccountsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return resource{r.client}.get(ctx, r.client.managementPath("/v1/accounts"), withQuery(query, opts)...)
}

func (r *AccountsResource) Get(ctx context.Context, accountID string, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("account_id", accountID); err != nil {
		return nil, err
	}
	return resource{r.client}.get(ctx, r.client.managementPath("/v1/accounts/"+pathEscape(accountID)), opts...)
}

type UsersResource struct {
	client *Client
}

func (r *UsersResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/users", body, opts...)
}

func (r *UsersResource) Update(ctx context.Context, userID string, body any, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	return patchInAccount(ctx, r.client, "/users/"+pathEscape(userID), body, opts...)
}

func (r *UsersResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	opts = withQuery(query, opts)
	res := resource{r.client}
	path, err := res.accountPath(opts, func(accountID string) string { return "/v1/accounts/" + accountID + "/users" })
	if err != nil {
		return nil, err
	}
	return res.get(ctx, path, opts...)
}

func (r *UsersResource) Get(ctx context.Context, userID string, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	res := resource{r.client}
	path, err := res.accountPath(opts, func(accountID string) string {
		return "/v1/accounts/" + accountID + "/users/" + pathEscape(userID)
	})
	if err != nil {
		return nil, err
	}
	return res.get(ctx, path, opts...)
}

type APIKeysResource struct {
	client *Client
}

func (r *APIKeysResource) Create(ctx context.Context, userID string, body any, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withAccountFromBody(body, opts)
	res := resource{r.client}
	path, err := res.accountPath(opts, func(accountID string) string {
		return "/v1/accounts/" + accountID + "/users/" + pathEscape(userID) + "/apiKeys"
	})
	if err != nil {
		return nil, err
	}
	return res.post(ctx, path, normalizeManagementBody(body), opts...)
}

func (r *APIKeysResource) List(ctx context.Context, userID string, query map[string]any, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withQuery(query, opts)
	res := resource{r.client}
	path, err := res.accountPath(opts, func(accountID string) string {
		return "/v1/accounts/" + accountID + "/users/" + pathEscape(userID) + "/apiKeys"
	})
	if err != nil {
		return nil, err
	}
	return res.get(ctx, path, opts...)
}

func (r *APIKeysResource) Delete(ctx context.Context, userID string, body any, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withAccountFromBody(body, opts)
	res := resource{r.client}
	path, err := res.accountPath(opts, func(accountID string) string {
		return "/v1/accounts/" + accountID + "/users/" + pathEscape(userID) + "/apiKeys:delete"
	})
	if err != nil {
		return nil, err
	}
	return res.post(ctx, path, normalizeManagementBody(body), opts...)
}

type BatchInferenceJobsResource struct {
	client *Client
}

func (r *BatchInferenceJobsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/batchInferenceJobs", body, opts...)
}

func (r *BatchInferenceJobsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/batchInferenceJobs", query, opts...)
}

func (r *BatchInferenceJobsResource) Get(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/batchInferenceJobs/"+pathEscape(jobID), opts...)
}

func (r *BatchInferenceJobsResource) Delete(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/batchInferenceJobs/"+pathEscape(jobID), opts...)
}

type DeploymentsResource struct {
	client *Client
}

func (r *DeploymentsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/deployments", body, opts...)
}

func (r *DeploymentsResource) Update(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID), body, opts...)
}

func (r *DeploymentsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/deployments", query, opts...)
}

func (r *DeploymentsResource) Get(ctx context.Context, deploymentID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID), opts...)
}

func (r *DeploymentsResource) Delete(ctx context.Context, deploymentID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID), opts...)
}

func (r *DeploymentsResource) Scale(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID)+":scale", body, opts...)
}

func (r *DeploymentsResource) Undelete(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID)+":undelete", body, opts...)
}

type DeploymentShapesResource struct {
	client *Client
}

func (r *DeploymentShapesResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/deploymentShapes", query, opts...)
}

func (r *DeploymentShapesResource) Get(ctx context.Context, shapeID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID), opts...)
}

type DeploymentShapeVersionsResource struct {
	client *Client
}

func (r *DeploymentShapeVersionsResource) List(ctx context.Context, shapeID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID)+"/versions", query, opts...)
}

func (r *DeploymentShapeVersionsResource) Get(ctx context.Context, shapeID, versionID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID)+"/versions/"+pathEscape(versionID), opts...)
}

type ModelsResource struct {
	client *Client
}

func (r *ModelsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/models", body, opts...)
}

func (r *ModelsResource) Update(ctx context.Context, modelID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/models/"+pathEscape(modelID), body, opts...)
}

func (r *ModelsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/models", query, opts...)
}

func (r *ModelsResource) Get(ctx context.Context, modelID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/models/"+pathEscape(modelID), opts...)
}

func (r *ModelsResource) Delete(ctx context.Context, modelID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/models/"+pathEscape(modelID), opts...)
}

func (r *ModelsResource) GetDownloadEndpoint(ctx context.Context, modelID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/models/"+pathEscape(modelID)+":getDownloadEndpoint", withQuery(query, opts)...)
}

func (r *ModelsResource) GetUploadEndpoint(ctx context.Context, modelID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/models/"+pathEscape(modelID)+":getUploadEndpoint", body, opts...)
}

func (r *ModelsResource) Prepare(ctx context.Context, modelID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/models/"+pathEscape(modelID)+":prepare", body, opts...)
}

func (r *ModelsResource) ValidateUpload(ctx context.Context, modelID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/models/"+pathEscape(modelID)+":validateUpload", withQuery(query, opts)...)
}

type LoraResource struct {
	client *Client
}

func (r *LoraResource) Update(ctx context.Context, deployedModelID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), body, opts...)
}

func (r *LoraResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/deployedModels", query, opts...)
}

func (r *LoraResource) Get(ctx context.Context, deployedModelID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), opts...)
}

func (r *LoraResource) Load(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/deployedModels", body, opts...)
}

func (r *LoraResource) Unload(ctx context.Context, deployedModelID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), opts...)
}

type DatasetsResource struct {
	client *Client
}

func (r *DatasetsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/datasets", body, opts...)
}

func (r *DatasetsResource) Update(ctx context.Context, datasetID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID), body, opts...)
}

func (r *DatasetsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/datasets", query, opts...)
}

func (r *DatasetsResource) Get(ctx context.Context, datasetID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID), opts...)
}

func (r *DatasetsResource) Delete(ctx context.Context, datasetID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID), opts...)
}

func (r *DatasetsResource) GetDownloadEndpoint(ctx context.Context, datasetID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID)+":getDownloadEndpoint", withQuery(query, opts)...)
}

func (r *DatasetsResource) GetUploadEndpoint(ctx context.Context, datasetID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID)+":getUploadEndpoint", body, opts...)
}

func (r *DatasetsResource) Upload(ctx context.Context, datasetID string, body any, opts ...RequestOption) (Response, error) {
	opts = withAccountFromBody(body, opts)
	if file, ok := uploadFileFromBody(body); ok {
		return r.UploadFile(ctx, datasetID, file, opts...)
	}
	return postInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID)+":upload", body, opts...)
}

func (r *DatasetsResource) UploadFile(ctx context.Context, datasetID string, file File, opts ...RequestOption) (Response, error) {
	res := resource{r.client}
	path, err := accountSuffixPath(res, opts, "/datasets/"+pathEscape(datasetID)+":upload")
	if err != nil {
		return nil, err
	}
	var out Response
	err = r.client.MultipartRequest(ctx, http.MethodPost, path, nil, map[string]File{"file": file}, &out, opts...)
	return out, err
}

func (r *DatasetsResource) ValidateUpload(ctx context.Context, datasetID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID)+":validateUpload", body, opts...)
}

type SupervisedFineTuningJobsResource struct {
	client *Client
}

func (r *SupervisedFineTuningJobsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/supervisedFineTuningJobs", body, opts...)
}

func (r *SupervisedFineTuningJobsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/supervisedFineTuningJobs", query, opts...)
}

func (r *SupervisedFineTuningJobsResource) Get(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *SupervisedFineTuningJobsResource) Delete(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *SupervisedFineTuningJobsResource) Resume(ctx context.Context, jobID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

type ReinforcementFineTuningJobsResource struct {
	client *Client
}

func (r *ReinforcementFineTuningJobsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/reinforcementFineTuningJobs", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/reinforcementFineTuningJobs", query, opts...)
}

func (r *ReinforcementFineTuningJobsResource) Get(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *ReinforcementFineTuningJobsResource) Delete(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *ReinforcementFineTuningJobsResource) Cancel(ctx context.Context, jobID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID)+":cancel", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) Resume(ctx context.Context, jobID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

type ReinforcementFineTuningStepsResource struct {
	client *Client
}

func (r *ReinforcementFineTuningStepsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/rlorTrainerJobs", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/rlorTrainerJobs", query, opts...)
}

func (r *ReinforcementFineTuningStepsResource) Get(ctx context.Context, trainerJobID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID), opts...)
}

func (r *ReinforcementFineTuningStepsResource) Delete(ctx context.Context, trainerJobID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID), opts...)
}

func (r *ReinforcementFineTuningStepsResource) Execute(ctx context.Context, trainerJobID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID)+":executeTrainStep", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) Resume(ctx context.Context, trainerJobID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID)+":resume", body, opts...)
}

type DPOJobsResource struct {
	client *Client
}

func (r *DPOJobsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/dpoJobs", body, opts...)
}

func (r *DPOJobsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/dpoJobs", query, opts...)
}

func (r *DPOJobsResource) Get(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/dpoJobs/"+pathEscape(jobID), opts...)
}

func (r *DPOJobsResource) Delete(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/dpoJobs/"+pathEscape(jobID), opts...)
}

func (r *DPOJobsResource) GetMetricsFileEndpoint(ctx context.Context, jobID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/dpoJobs/"+pathEscape(jobID)+":getMetricsFileEndpoint", withQuery(query, opts)...)
}

func (r *DPOJobsResource) Resume(ctx context.Context, jobID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/dpoJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

type EvaluationJobsResource struct {
	client *Client
}

func (r *EvaluationJobsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/evaluationJobs", body, opts...)
}

func (r *EvaluationJobsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/evaluationJobs", query, opts...)
}

func (r *EvaluationJobsResource) Get(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/evaluationJobs/"+pathEscape(jobID), opts...)
}

func (r *EvaluationJobsResource) Delete(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/evaluationJobs/"+pathEscape(jobID), opts...)
}

func (r *EvaluationJobsResource) GetLogEndpoint(ctx context.Context, jobID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/evaluationJobs/"+pathEscape(jobID)+":getExecutionLogEndpoint", withQuery(query, opts)...)
}

type EvaluatorsResource struct {
	client *Client
}

func (r *EvaluatorsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/evaluatorsV2", body, opts...)
}

func (r *EvaluatorsResource) Update(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), body, opts...)
}

func (r *EvaluatorsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/evaluators", query, opts...)
}

func (r *EvaluatorsResource) Get(ctx context.Context, evaluatorID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), opts...)
}

func (r *EvaluatorsResource) Delete(ctx context.Context, evaluatorID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), opts...)
}

func (r *EvaluatorsResource) GetBuildLogEndpoint(ctx context.Context, evaluatorID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":getBuildLogEndpoint", withQuery(query, opts)...)
}

func (r *EvaluatorsResource) GetSourceCodeEndpoint(ctx context.Context, evaluatorID string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":getSourceCodeSignedUrl", withQuery(query, opts)...)
}

func (r *EvaluatorsResource) GetUploadEndpoint(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":getUploadEndpoint", body, opts...)
}

func (r *EvaluatorsResource) ValidateUpload(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":validateUpload", body, opts...)
}

type SecretsResource struct {
	client *Client
}

func (r *SecretsResource) Create(ctx context.Context, body any, opts ...RequestOption) (Response, error) {
	return createInAccount(ctx, r.client, "/secrets", body, opts...)
}

func (r *SecretsResource) Update(ctx context.Context, secretID string, body any, opts ...RequestOption) (Response, error) {
	return patchInAccount(ctx, r.client, "/secrets/"+pathEscape(secretID), body, opts...)
}

func (r *SecretsResource) List(ctx context.Context, query map[string]any, opts ...RequestOption) (Response, error) {
	return listInAccount(ctx, r.client, "/secrets", query, opts...)
}

func (r *SecretsResource) Get(ctx context.Context, secretID string, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, r.client, "/secrets/"+pathEscape(secretID), opts...)
}

func (r *SecretsResource) Delete(ctx context.Context, secretID string, opts ...RequestOption) (Response, error) {
	return deleteInAccount(ctx, r.client, "/secrets/"+pathEscape(secretID), opts...)
}

func createInAccount(ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (Response, error) {
	return postInAccount(ctx, client, suffix, body, opts...)
}

func listInAccount(ctx context.Context, client *Client, suffix string, query map[string]any, opts ...RequestOption) (Response, error) {
	return getInAccount(ctx, client, suffix, withQuery(query, opts)...)
}

func getInAccount(ctx context.Context, client *Client, suffix string, opts ...RequestOption) (Response, error) {
	res := resource{client}
	path, err := accountSuffixPath(res, opts, suffix)
	if err != nil {
		return nil, err
	}
	return res.get(ctx, path, opts...)
}

func postInAccount(ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (Response, error) {
	opts = withAccountFromBody(body, opts)
	res := resource{client}
	path, err := accountSuffixPath(res, opts, suffix)
	if err != nil {
		return nil, err
	}
	return res.post(ctx, path, normalizeManagementBody(body), opts...)
}

func patchInAccount(ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (Response, error) {
	opts = withAccountFromBody(body, opts)
	res := resource{client}
	path, err := accountSuffixPath(res, opts, suffix)
	if err != nil {
		return nil, err
	}
	return res.patch(ctx, path, normalizeManagementBody(body), opts...)
}

func deleteInAccount(ctx context.Context, client *Client, suffix string, opts ...RequestOption) (Response, error) {
	res := resource{client}
	path, err := accountSuffixPath(res, opts, suffix)
	if err != nil {
		return nil, err
	}
	return res.delete(ctx, path, opts...)
}

func accountSuffixPath(res resource, opts []RequestOption, suffix string) (string, error) {
	if err := validatePathSuffix(suffix); err != nil {
		return "", err
	}
	return res.accountPath(opts, func(accountID string) string {
		return "/v1/accounts/" + accountID + suffix
	})
}

func validatePathSuffix(suffix string) error {
	if suffix == "" || !strings.HasPrefix(suffix, "/") || strings.Contains(suffix, "//") || strings.HasSuffix(suffix, "/") || strings.Contains(suffix, "/:") {
		return &Error{Message: "missing required path argument"}
	}
	return nil
}

func requirePathArgument(name, value string) error {
	if value == "" {
		return &Error{Message: "missing required path argument: " + name}
	}
	return nil
}
