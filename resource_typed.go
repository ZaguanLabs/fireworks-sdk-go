package fireworks

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

func typedGet[T any](ctx context.Context, client *Client, path string, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodGet, path, nil, &out, opts...)
	return &out, err
}

func typedPost[T any](ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodPost, path, normalizeManagementBody(body), &out, opts...)
	return &out, err
}

func typedPatch[T any](ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodPatch, path, normalizeManagementBody(body), &out, opts...)
	return &out, err
}

func typedDelete[T any](ctx context.Context, client *Client, path string, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodDelete, path, nil, &out, opts...)
	return &out, err
}

func typedPostResponse(ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := client.Request(ctx, http.MethodPost, path, normalizeManagementBody(body), &out, opts...)
	return out, err
}

func typedPatchResponse(ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	err := client.Request(ctx, http.MethodPatch, path, normalizeManagementBody(body), &out, opts...)
	return out, err
}

func typedDeleteResponse(ctx context.Context, client *Client, path string, opts ...RequestOption) (Response, error) {
	var out Response
	err := client.Request(ctx, http.MethodDelete, path, nil, &out, opts...)
	return out, err
}

func typedAccountPath(client *Client, suffix string, opts []RequestOption) (string, error) {
	return accountSuffixPath(resource{client}, opts, suffix)
}

func (r *AccountsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.AccountsPage, error) {
	return typedGet[fwtypes.AccountsPage](ctx, r.client, r.client.managementPath("/v1/accounts"), withQuery(query, opts)...)
}

func (r *AccountsResource) GetTyped(ctx context.Context, accountID string, query any, opts ...RequestOption) (*fwtypes.Account, error) {
	if err := requirePathArgument("account_id", accountID); err != nil {
		return nil, err
	}
	return typedGet[fwtypes.Account](ctx, r.client, r.client.managementPath("/v1/accounts/"+pathEscape(accountID)), withQuery(query, opts)...)
}

func (r *UsersResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.User, error) {
	return typedPostInAccountWithQuery[fwtypes.User](ctx, r.client, "/users", body, []string{"user_id"}, opts...)
}

func (r *UsersResource) UpdateTyped(ctx context.Context, userID string, body any, opts ...RequestOption) (*fwtypes.User, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID), opts)
	if err != nil {
		return nil, err
	}
	return typedPatch[fwtypes.User](ctx, r.client, path, body, opts...)
}

func (r *UsersResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.UsersPage, error) {
	opts = withQuery(query, opts)
	path, err := typedAccountPath(r.client, "/users", opts)
	if err != nil {
		return nil, err
	}
	return typedGet[fwtypes.UsersPage](ctx, r.client, path, opts...)
}

func (r *UsersResource) GetTyped(ctx context.Context, userID string, query any, opts ...RequestOption) (*fwtypes.User, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withQuery(query, opts)
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID), opts)
	if err != nil {
		return nil, err
	}
	return typedGet[fwtypes.User](ctx, r.client, path, opts...)
}

func (r *APIKeysResource) CreateTyped(ctx context.Context, userID string, body any, opts ...RequestOption) (*fwtypes.APIKey, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID)+"/apiKeys", opts)
	if err != nil {
		return nil, err
	}
	return typedPost[fwtypes.APIKey](ctx, r.client, path, body, opts...)
}

func (r *APIKeysResource) ListTyped(ctx context.Context, userID string, query any, opts ...RequestOption) (*fwtypes.APIKeysPage, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withQuery(query, opts)
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID)+"/apiKeys", opts)
	if err != nil {
		return nil, err
	}
	return typedGet[fwtypes.APIKeysPage](ctx, r.client, path, opts...)
}

func (r *APIKeysResource) DeleteTyped(ctx context.Context, userID string, body any, opts ...RequestOption) (Response, error) {
	if err := requirePathArgument("user_id", userID); err != nil {
		return nil, err
	}
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID)+"/apiKeys:delete", opts)
	if err != nil {
		return nil, err
	}
	return typedPostResponse(ctx, r.client, path, body, opts...)
}

func (r *ModelsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.Model, error) {
	return typedPostInAccount[fwtypes.Model](ctx, r.client, "/models", body, opts...)
}

func (r *ModelsResource) UpdateTyped(ctx context.Context, modelID string, body any, opts ...RequestOption) (*fwtypes.Model, error) {
	return typedPatchInAccount[fwtypes.Model](ctx, r.client, "/models/"+pathEscape(modelID), body, opts...)
}

func (r *ModelsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.ModelsPage, error) {
	return typedListInAccount[fwtypes.ModelsPage](ctx, r.client, "/models", query, opts...)
}

func (r *ModelsResource) GetTyped(ctx context.Context, modelID string, query any, opts ...RequestOption) (*fwtypes.Model, error) {
	return typedGetInAccount[fwtypes.Model](ctx, r.client, "/models/"+pathEscape(modelID), withQuery(query, opts)...)
}

func (r *ModelsResource) DeleteTyped(ctx context.Context, modelID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/models/"+pathEscape(modelID), opts...)
}

func (r *ModelsResource) GetDownloadEndpointTyped(ctx context.Context, modelID string, query any, opts ...RequestOption) (*fwtypes.ModelGetDownloadEndpointResponse, error) {
	return typedGetInAccount[fwtypes.ModelGetDownloadEndpointResponse](ctx, r.client, "/models/"+pathEscape(modelID)+":getDownloadEndpoint", withQuery(query, opts)...)
}

func (r *ModelsResource) GetUploadEndpointTyped(ctx context.Context, modelID string, body any, opts ...RequestOption) (*fwtypes.ModelGetUploadEndpointResponse, error) {
	return typedPostInAccount[fwtypes.ModelGetUploadEndpointResponse](ctx, r.client, "/models/"+pathEscape(modelID)+":getUploadEndpoint", body, opts...)
}

func (r *ModelsResource) PrepareTyped(ctx context.Context, modelID string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(r.client, "/models/"+pathEscape(modelID)+":prepare", opts)
	if err != nil {
		return nil, err
	}
	err = r.client.Request(ctx, http.MethodPost, path, normalizeManagementBody(body), &out, opts...)
	return out, err
}

func (r *ModelsResource) ValidateUploadTyped(ctx context.Context, modelID string, query any, opts ...RequestOption) (*fwtypes.ModelValidateUploadResponse, error) {
	return typedGetInAccount[fwtypes.ModelValidateUploadResponse](ctx, r.client, "/models/"+pathEscape(modelID)+":validateUpload", withQuery(query, opts)...)
}

func (r *DatasetsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.Dataset, error) {
	return typedPostInAccount[fwtypes.Dataset](ctx, r.client, "/datasets", body, opts...)
}

func (r *DatasetsResource) UpdateTyped(ctx context.Context, datasetID string, body any, opts ...RequestOption) (*fwtypes.Dataset, error) {
	return typedPatchInAccount[fwtypes.Dataset](ctx, r.client, "/datasets/"+pathEscape(datasetID), body, opts...)
}

func (r *DatasetsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DatasetsPage, error) {
	return typedListInAccount[fwtypes.DatasetsPage](ctx, r.client, "/datasets", query, opts...)
}

func (r *DatasetsResource) GetTyped(ctx context.Context, datasetID string, query any, opts ...RequestOption) (*fwtypes.Dataset, error) {
	return typedGetInAccount[fwtypes.Dataset](ctx, r.client, "/datasets/"+pathEscape(datasetID), withQuery(query, opts)...)
}

func (r *DatasetsResource) DeleteTyped(ctx context.Context, datasetID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/datasets/"+pathEscape(datasetID), opts...)
}

func (r *DatasetsResource) GetDownloadEndpointTyped(ctx context.Context, datasetID string, query any, opts ...RequestOption) (*fwtypes.DatasetGetDownloadEndpointResponse, error) {
	return typedGetInAccount[fwtypes.DatasetGetDownloadEndpointResponse](ctx, r.client, "/datasets/"+pathEscape(datasetID)+":getDownloadEndpoint", withQuery(query, opts)...)
}

func (r *DatasetsResource) GetUploadEndpointTyped(ctx context.Context, datasetID string, body any, opts ...RequestOption) (*fwtypes.DatasetGetUploadEndpointResponse, error) {
	return typedPostInAccount[fwtypes.DatasetGetUploadEndpointResponse](ctx, r.client, "/datasets/"+pathEscape(datasetID)+":getUploadEndpoint", body, opts...)
}

func (r *DatasetsResource) UploadTyped(ctx context.Context, datasetID string, body any, opts ...RequestOption) (*fwtypes.DatasetUploadResponse, error) {
	opts = withAccountFromBody(body, opts)
	if file, ok := uploadFileFromBody(body); ok {
		return r.UploadFileTyped(ctx, datasetID, file, opts...)
	}
	return typedPostInAccount[fwtypes.DatasetUploadResponse](ctx, r.client, "/datasets/"+pathEscape(datasetID)+":upload", body, opts...)
}

func (r *DatasetsResource) UploadFileTyped(ctx context.Context, datasetID string, file File, opts ...RequestOption) (*fwtypes.DatasetUploadResponse, error) {
	path, err := typedAccountPath(r.client, "/datasets/"+pathEscape(datasetID)+":upload", opts)
	if err != nil {
		return nil, err
	}
	var out fwtypes.DatasetUploadResponse
	err = r.client.MultipartRequest(ctx, http.MethodPost, path, nil, map[string]File{"file": file}, &out, opts...)
	return &out, err
}

func (r *DatasetsResource) ValidateUploadTyped(ctx context.Context, datasetID string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(r.client, "/datasets/"+pathEscape(datasetID)+":validateUpload", opts)
	if err != nil {
		return nil, err
	}
	err = r.client.Request(ctx, http.MethodPost, path, normalizeManagementBody(body), &out, opts...)
	return out, err
}

func (r *DeploymentsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPostInAccountWithQuery[fwtypes.Deployment](ctx, r.client, "/deployments", body, []string{
		"deployment_id",
		"disable_auto_deploy",
		"disable_speculative_decoding",
		"skip_image_tag_validation",
		"skip_shape_validation",
		"validate_only",
	}, opts...)
}

func (r *DeploymentsResource) UpdateTyped(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPatchInAccountWithQuery[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID), body, []string{"skip_shape_validation"}, opts...)
}

func (r *DeploymentsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DeploymentsPage, error) {
	return typedListInAccount[fwtypes.DeploymentsPage](ctx, r.client, "/deployments", query, opts...)
}

func (r *DeploymentsResource) GetTyped(ctx context.Context, deploymentID string, query any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedGetInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID), withQuery(query, opts)...)
}

func (r *DeploymentsResource) DeleteTyped(ctx context.Context, deploymentID string, query any, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID), withQuery(query, opts)...)
}

func (r *DeploymentsResource) ScaleTyped(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (Response, error) {
	return typedPatchResponseInAccount(ctx, r.client, "/deployments/"+pathEscape(deploymentID)+":scale", body, opts...)
}

func (r *DeploymentsResource) UndeleteTyped(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPostInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID)+":undelete", body, opts...)
}

func (r *DeploymentShapesResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DeploymentShapesPage, error) {
	return typedListInAccount[fwtypes.DeploymentShapesPage](ctx, r.client, "/deploymentShapes", query, opts...)
}

func (r *DeploymentShapesResource) GetTyped(ctx context.Context, shapeID string, query any, opts ...RequestOption) (*fwtypes.DeploymentShape, error) {
	return typedGetInAccount[fwtypes.DeploymentShape](ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID), withQuery(query, opts)...)
}

func (r *DeploymentShapeVersionsResource) ListTyped(ctx context.Context, shapeID string, query any, opts ...RequestOption) (*fwtypes.DeploymentShapeVersionsPage, error) {
	return typedListInAccount[fwtypes.DeploymentShapeVersionsPage](ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID)+"/versions", query, opts...)
}

func (r *DeploymentShapeVersionsResource) GetTyped(ctx context.Context, shapeID, versionID string, query any, opts ...RequestOption) (*fwtypes.DeploymentShapeVersion, error) {
	return typedGetInAccount[fwtypes.DeploymentShapeVersion](ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID)+"/versions/"+pathEscape(versionID), withQuery(query, opts)...)
}

func (r *LoraResource) UpdateTyped(ctx context.Context, deployedModelID string, body any, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedPatchInAccount[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), body, opts...)
}

func (r *LoraResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.LoraPage, error) {
	return typedListInAccount[fwtypes.LoraPage](ctx, r.client, "/deployedModels", query, opts...)
}

func (r *LoraResource) GetTyped(ctx context.Context, deployedModelID string, query any, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedGetInAccount[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), withQuery(query, opts)...)
}

func (r *LoraResource) LoadTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedPostInAccountWithQuery[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels", body, []string{"replace_merged_addon"}, opts...)
}

func (r *LoraResource) UnloadTyped(ctx context.Context, deployedModelID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), opts...)
}

func (r *BatchInferenceJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.BatchInferenceJob, error) {
	return typedPostInAccountWithQuery[fwtypes.BatchInferenceJob](ctx, r.client, "/batchInferenceJobs", body, []string{"batch_inference_job_id"}, opts...)
}

func (r *BatchInferenceJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.BatchInferenceJobsPage, error) {
	return typedListInAccount[fwtypes.BatchInferenceJobsPage](ctx, r.client, "/batchInferenceJobs", query, opts...)
}

func (r *BatchInferenceJobsResource) GetTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.BatchInferenceJob, error) {
	return typedGetInAccount[fwtypes.BatchInferenceJob](ctx, r.client, "/batchInferenceJobs/"+pathEscape(jobID), withQuery(query, opts)...)
}

func (r *BatchInferenceJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/batchInferenceJobs/"+pathEscape(jobID), opts...)
}

func (r *SecretsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.Secret, error) {
	return typedPostInAccount[fwtypes.Secret](ctx, r.client, "/secrets", body, opts...)
}

func (r *SecretsResource) UpdateTyped(ctx context.Context, secretID string, body any, opts ...RequestOption) (*fwtypes.Secret, error) {
	return typedPatchInAccount[fwtypes.Secret](ctx, r.client, "/secrets/"+pathEscape(secretID), body, opts...)
}

func (r *SecretsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.SecretsPage, error) {
	return typedListInAccount[fwtypes.SecretsPage](ctx, r.client, "/secrets", query, opts...)
}

func (r *SecretsResource) GetTyped(ctx context.Context, secretID string, query any, opts ...RequestOption) (*fwtypes.Secret, error) {
	return typedGetInAccount[fwtypes.Secret](ctx, r.client, "/secrets/"+pathEscape(secretID), withQuery(query, opts)...)
}

func (r *SecretsResource) DeleteTyped(ctx context.Context, secretID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/secrets/"+pathEscape(secretID), opts...)
}

func (r *SupervisedFineTuningJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedPostInAccountWithQuery[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs", body, []string{"supervised_fine_tuning_job_id"}, opts...)
}

func (r *SupervisedFineTuningJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJobsPage, error) {
	return typedListInAccount[fwtypes.SupervisedFineTuningJobsPage](ctx, r.client, "/supervisedFineTuningJobs", query, opts...)
}

func (r *SupervisedFineTuningJobsResource) GetTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedGetInAccount[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID), withQuery(query, opts)...)
}

func (r *SupervisedFineTuningJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *SupervisedFineTuningJobsResource) ResumeTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedPostInAccount[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedPostInAccountWithQuery[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs", body, []string{"reinforcement_fine_tuning_job_id"}, opts...)
}

func (r *ReinforcementFineTuningJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJobsPage, error) {
	return typedListInAccount[fwtypes.ReinforcementFineTuningJobsPage](ctx, r.client, "/reinforcementFineTuningJobs", query, opts...)
}

func (r *ReinforcementFineTuningJobsResource) GetTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedGetInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID), withQuery(query, opts)...)
}

func (r *ReinforcementFineTuningJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *ReinforcementFineTuningJobsResource) CancelTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (Response, error) {
	return typedPostResponseInAccount(ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID)+":cancel", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) ResumeTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedPostInAccountWithQuery[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs", body, []string{"rlor_trainer_job_id"}, opts...)
}

func (r *ReinforcementFineTuningStepsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStepsPage, error) {
	return typedListInAccount[fwtypes.ReinforcementFineTuningStepsPage](ctx, r.client, "/rlorTrainerJobs", query, opts...)
}

func (r *ReinforcementFineTuningStepsResource) GetTyped(ctx context.Context, trainerJobID string, query any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedGetInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID), withQuery(query, opts)...)
}

func (r *ReinforcementFineTuningStepsResource) DeleteTyped(ctx context.Context, trainerJobID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID), opts...)
}

func (r *ReinforcementFineTuningStepsResource) ExecuteTyped(ctx context.Context, trainerJobID string, body any, opts ...RequestOption) (Response, error) {
	return typedPostResponseInAccount(ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID)+":executeTrainStep", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) ResumeTyped(ctx context.Context, trainerJobID string, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID)+":resume", body, opts...)
}

func (r *DPOJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.DpoJob, error) {
	return typedPostInAccountWithQuery[fwtypes.DpoJob](ctx, r.client, "/dpoJobs", body, []string{"dpo_job_id"}, opts...)
}

func (r *DPOJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DPOJobsPage, error) {
	return typedListInAccount[fwtypes.DPOJobsPage](ctx, r.client, "/dpoJobs", query, opts...)
}

func (r *DPOJobsResource) GetTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.DpoJob, error) {
	return typedGetInAccount[fwtypes.DpoJob](ctx, r.client, "/dpoJobs/"+pathEscape(jobID), withQuery(query, opts)...)
}

func (r *DPOJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/dpoJobs/"+pathEscape(jobID), opts...)
}

func (r *DPOJobsResource) GetMetricsFileEndpointTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.DPOJobGetMetricsFileEndpointResponse, error) {
	return typedGetInAccount[fwtypes.DPOJobGetMetricsFileEndpointResponse](ctx, r.client, "/dpoJobs/"+pathEscape(jobID)+":getMetricsFileEndpoint", opts...)
}

func (r *DPOJobsResource) ResumeTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (*fwtypes.DpoJob, error) {
	return typedPostInAccount[fwtypes.DpoJob](ctx, r.client, "/dpoJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

func (r *EvaluationJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.EvaluationJobCreateResponse, error) {
	return typedPostInAccount[fwtypes.EvaluationJobCreateResponse](ctx, r.client, "/evaluationJobs", body, opts...)
}

func (r *EvaluationJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.EvaluationJobsPage, error) {
	return typedListInAccount[fwtypes.EvaluationJobsPage](ctx, r.client, "/evaluationJobs", query, opts...)
}

func (r *EvaluationJobsResource) GetTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.EvaluationJobGetResponse, error) {
	return typedGetInAccount[fwtypes.EvaluationJobGetResponse](ctx, r.client, "/evaluationJobs/"+pathEscape(jobID), withQuery(query, opts)...)
}

func (r *EvaluationJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/evaluationJobs/"+pathEscape(jobID), opts...)
}

func (r *EvaluationJobsResource) GetLogEndpointTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.EvaluationJobGetLogEndpointResponse, error) {
	return typedGetInAccount[fwtypes.EvaluationJobGetLogEndpointResponse](ctx, r.client, "/evaluationJobs/"+pathEscape(jobID)+":getExecutionLogEndpoint", withQuery(query, opts)...)
}

func (r *EvaluatorsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.EvaluatorCreateResponse, error) {
	return typedPostInAccount[fwtypes.EvaluatorCreateResponse](ctx, r.client, "/evaluatorsV2", body, opts...)
}

func (r *EvaluatorsResource) UpdateTyped(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (*fwtypes.EvaluatorUpdateResponse, error) {
	return typedPatchInAccountWithQuery[fwtypes.EvaluatorUpdateResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), body, []string{"prepare_code_upload"}, opts...)
}

func (r *EvaluatorsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.EvaluatorsPage, error) {
	return typedListInAccount[fwtypes.EvaluatorsPage](ctx, r.client, "/evaluators", query, opts...)
}

func (r *EvaluatorsResource) GetTyped(ctx context.Context, evaluatorID string, query any, opts ...RequestOption) (*fwtypes.EvaluatorGetResponse, error) {
	return typedGetInAccount[fwtypes.EvaluatorGetResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), withQuery(query, opts)...)
}

func (r *EvaluatorsResource) DeleteTyped(ctx context.Context, evaluatorID string, opts ...RequestOption) (Response, error) {
	return typedDeleteResponseInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), opts...)
}

func (r *EvaluatorsResource) GetBuildLogEndpointTyped(ctx context.Context, evaluatorID string, query any, opts ...RequestOption) (*fwtypes.EvaluatorGetBuildLogEndpointResponse, error) {
	return typedGetInAccount[fwtypes.EvaluatorGetBuildLogEndpointResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":getBuildLogEndpoint", withQuery(query, opts)...)
}

func (r *EvaluatorsResource) GetSourceCodeEndpointTyped(ctx context.Context, evaluatorID string, query any, opts ...RequestOption) (*fwtypes.EvaluatorGetSourceCodeEndpointResponse, error) {
	return typedGetInAccount[fwtypes.EvaluatorGetSourceCodeEndpointResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":getSourceCodeSignedUrl", withQuery(query, opts)...)
}

func (r *EvaluatorsResource) GetUploadEndpointTyped(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (*fwtypes.EvaluatorGetUploadEndpointResponse, error) {
	return typedPostInAccount[fwtypes.EvaluatorGetUploadEndpointResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":getUploadEndpoint", body, opts...)
}

func (r *EvaluatorsResource) ValidateUploadTyped(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (Response, error) {
	return typedPostResponseInAccount(ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":validateUpload", body, opts...)
}

func typedListInAccount[T any](ctx context.Context, client *Client, suffix string, query any, opts ...RequestOption) (*T, error) {
	return typedGetInAccount[T](ctx, client, suffix, withQuery(query, opts)...)
}

func typedGetInAccount[T any](ctx context.Context, client *Client, suffix string, opts ...RequestOption) (*T, error) {
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedGet[T](ctx, client, path, opts...)
}

func typedPostInAccount[T any](ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (*T, error) {
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPost[T](ctx, client, path, body, opts...)
}

func typedPostInAccountWithQuery[T any](ctx context.Context, client *Client, suffix string, body any, queryKeys []string, opts ...RequestOption) (*T, error) {
	opts = withAccountFromBody(body, opts)
	body, query := splitBodyQuery(body, queryKeys...)
	opts = withQuery(query, opts)
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPost[T](ctx, client, path, body, opts...)
}

func typedPatchInAccount[T any](ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (*T, error) {
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPatch[T](ctx, client, path, body, opts...)
}

func typedPatchInAccountWithQuery[T any](ctx context.Context, client *Client, suffix string, body any, queryKeys []string, opts ...RequestOption) (*T, error) {
	opts = withAccountFromBody(body, opts)
	body, query := splitBodyQuery(body, queryKeys...)
	opts = withQuery(query, opts)
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPatch[T](ctx, client, path, body, opts...)
}

func typedDeleteInAccount[T any](ctx context.Context, client *Client, suffix string, opts ...RequestOption) (*T, error) {
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedDelete[T](ctx, client, path, opts...)
}

func typedPostResponseInAccount(ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (Response, error) {
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPostResponse(ctx, client, path, body, opts...)
}

func typedPatchResponseInAccount(ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (Response, error) {
	opts = withAccountFromBody(body, opts)
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPatchResponse(ctx, client, path, body, opts...)
}

func typedDeleteResponseInAccount(ctx context.Context, client *Client, suffix string, opts ...RequestOption) (Response, error) {
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedDeleteResponse(ctx, client, path, opts...)
}

func withAccountFromBody(body any, opts []RequestOption) []RequestOption {
	accountID := accountIDFromBody(body)
	if accountID == "" {
		return opts
	}
	out := make([]RequestOption, 0, len(opts)+1)
	out = append(out, WithAccountID(accountID))
	out = append(out, opts...)
	return out
}

func accountIDFromBody(body any) string {
	switch v := body.(type) {
	case nil:
		return ""
	case map[string]any:
		if accountID, ok := v["account_id"].(string); ok {
			return accountID
		}
		return ""
	case fwtypes.DatasetUploadParams:
		return v.AccountID
	case *fwtypes.DatasetUploadParams:
		if v == nil {
			return ""
		}
		return v.AccountID
	default:
		payload, err := openapiMarshal(body)
		if err != nil {
			return ""
		}
		var values map[string]any
		if err := json.Unmarshal(payload, &values); err != nil {
			return ""
		}
		accountID, _ := values["account_id"].(string)
		return accountID
	}
}

func normalizeManagementBody(body any) any {
	switch v := body.(type) {
	case nil:
		return nil
	case map[string]any:
		return normalizeManagementValue(v)
	case JSON:
		return normalizeManagementValue(map[string]any(v))
	default:
		payload, err := openapiMarshal(body)
		if err != nil || string(payload) == "null" {
			return body
		}
		var values any
		if err := json.Unmarshal(payload, &values); err != nil {
			return body
		}
		return normalizeManagementValue(values)
	}
}

func splitBodyQuery(body any, queryKeys ...string) (any, map[string]any) {
	body = normalizeManagementBody(body)
	bodyMap, ok := body.(map[string]any)
	if !ok {
		if jsonBody, ok := body.(JSON); ok {
			bodyMap = map[string]any(jsonBody)
		} else {
			return body, nil
		}
	}

	out := make(map[string]any, len(bodyMap))
	for key, value := range bodyMap {
		out[key] = value
	}
	query := make(map[string]any)
	for _, key := range queryKeys {
		queryKey := queryAlias(key)
		for _, candidate := range []string{queryKey, key} {
			value, ok := out[candidate]
			if !ok {
				continue
			}
			delete(out, candidate)
			if value != nil {
				query[queryKey] = value
			}
			break
		}
	}
	if len(query) == 0 {
		return out, nil
	}
	return out, query
}

func normalizeManagementValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if key == "account_id" {
				continue
			}
			out[managementBodyAlias(key)] = normalizeManagementValue(item)
		}
		return out
	case JSON:
		return normalizeManagementValue(map[string]any(v))
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeManagementValue(item))
		}
		return out
	default:
		return value
	}
}

func managementBodyAlias(key string) string {
	if !strings.Contains(key, "_") {
		return key
	}
	parts := strings.Split(key, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}
