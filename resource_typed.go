package fireworks

import (
	"context"
	"net/http"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

func typedGet[T any](ctx context.Context, client *Client, path string, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodGet, path, nil, &out, opts...)
	return &out, err
}

func typedPost[T any](ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodPost, path, body, &out, opts...)
	return &out, err
}

func typedPatch[T any](ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodPatch, path, body, &out, opts...)
	return &out, err
}

func typedDelete[T any](ctx context.Context, client *Client, path string, opts ...RequestOption) (*T, error) {
	var out T
	err := client.Request(ctx, http.MethodDelete, path, nil, &out, opts...)
	return &out, err
}

func typedAccountPath(client *Client, suffix string, opts []RequestOption) (string, error) {
	return accountSuffixPath(resource{client}, opts, suffix)
}

func (r *AccountsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.AccountsPage, error) {
	return typedGet[fwtypes.AccountsPage](ctx, r.client, r.client.managementPath("/v1/accounts"), withQuery(query, opts)...)
}

func (r *AccountsResource) GetTyped(ctx context.Context, accountID string, opts ...RequestOption) (*fwtypes.Account, error) {
	return typedGet[fwtypes.Account](ctx, r.client, r.client.managementPath("/v1/accounts/"+pathEscape(accountID)), opts...)
}

func (r *UsersResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.User, error) {
	path, err := typedAccountPath(r.client, "/users", opts)
	if err != nil {
		return nil, err
	}
	return typedPost[fwtypes.User](ctx, r.client, path, body, opts...)
}

func (r *UsersResource) UpdateTyped(ctx context.Context, userID string, body any, opts ...RequestOption) (*fwtypes.User, error) {
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID), opts)
	if err != nil {
		return nil, err
	}
	return typedPatch[fwtypes.User](ctx, r.client, path, body, opts...)
}

func (r *UsersResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.UsersPage, error) {
	path, err := typedAccountPath(r.client, "/users", opts)
	if err != nil {
		return nil, err
	}
	return typedGet[fwtypes.UsersPage](ctx, r.client, path, withQuery(query, opts)...)
}

func (r *UsersResource) GetTyped(ctx context.Context, userID string, opts ...RequestOption) (*fwtypes.User, error) {
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID), opts)
	if err != nil {
		return nil, err
	}
	return typedGet[fwtypes.User](ctx, r.client, path, opts...)
}

func (r *APIKeysResource) CreateTyped(ctx context.Context, userID string, body any, opts ...RequestOption) (*fwtypes.APIKey, error) {
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID)+"/apiKeys", opts)
	if err != nil {
		return nil, err
	}
	return typedPost[fwtypes.APIKey](ctx, r.client, path, body, opts...)
}

func (r *APIKeysResource) ListTyped(ctx context.Context, userID string, query any, opts ...RequestOption) (*fwtypes.APIKeysPage, error) {
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID)+"/apiKeys", opts)
	if err != nil {
		return nil, err
	}
	return typedGet[fwtypes.APIKeysPage](ctx, r.client, path, withQuery(query, opts)...)
}

func (r *APIKeysResource) DeleteTyped(ctx context.Context, userID string, body any, opts ...RequestOption) (*fwtypes.APIKey, error) {
	path, err := typedAccountPath(r.client, "/users/"+pathEscape(userID)+"/apiKeys:delete", opts)
	if err != nil {
		return nil, err
	}
	return typedPost[fwtypes.APIKey](ctx, r.client, path, body, opts...)
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

func (r *ModelsResource) GetTyped(ctx context.Context, modelID string, opts ...RequestOption) (*fwtypes.Model, error) {
	return typedGetInAccount[fwtypes.Model](ctx, r.client, "/models/"+pathEscape(modelID), opts...)
}

func (r *ModelsResource) DeleteTyped(ctx context.Context, modelID string, opts ...RequestOption) (*fwtypes.Model, error) {
	return typedDeleteInAccount[fwtypes.Model](ctx, r.client, "/models/"+pathEscape(modelID), opts...)
}

func (r *ModelsResource) GetDownloadEndpointTyped(ctx context.Context, modelID string, query any, opts ...RequestOption) (*fwtypes.ModelGetDownloadEndpointResponse, error) {
	return typedGetInAccount[fwtypes.ModelGetDownloadEndpointResponse](ctx, r.client, "/models/"+pathEscape(modelID)+":getDownloadEndpoint", withQuery(query, opts)...)
}

func (r *ModelsResource) GetUploadEndpointTyped(ctx context.Context, modelID string, body any, opts ...RequestOption) (*fwtypes.ModelGetUploadEndpointResponse, error) {
	return typedPostInAccount[fwtypes.ModelGetUploadEndpointResponse](ctx, r.client, "/models/"+pathEscape(modelID)+":getUploadEndpoint", body, opts...)
}

func (r *ModelsResource) PrepareTyped(ctx context.Context, modelID string, body any, opts ...RequestOption) (Response, error) {
	var out Response
	path, err := typedAccountPath(r.client, "/models/"+pathEscape(modelID)+":prepare", opts)
	if err != nil {
		return nil, err
	}
	err = r.client.Request(ctx, http.MethodPost, path, body, &out, opts...)
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

func (r *DatasetsResource) GetTyped(ctx context.Context, datasetID string, opts ...RequestOption) (*fwtypes.Dataset, error) {
	return typedGetInAccount[fwtypes.Dataset](ctx, r.client, "/datasets/"+pathEscape(datasetID), opts...)
}

func (r *DatasetsResource) DeleteTyped(ctx context.Context, datasetID string, opts ...RequestOption) (*fwtypes.Dataset, error) {
	return typedDeleteInAccount[fwtypes.Dataset](ctx, r.client, "/datasets/"+pathEscape(datasetID), opts...)
}

func (r *DatasetsResource) GetDownloadEndpointTyped(ctx context.Context, datasetID string, query any, opts ...RequestOption) (*fwtypes.DatasetGetDownloadEndpointResponse, error) {
	return typedGetInAccount[fwtypes.DatasetGetDownloadEndpointResponse](ctx, r.client, "/datasets/"+pathEscape(datasetID)+":getDownloadEndpoint", withQuery(query, opts)...)
}

func (r *DatasetsResource) GetUploadEndpointTyped(ctx context.Context, datasetID string, body any, opts ...RequestOption) (*fwtypes.DatasetGetUploadEndpointResponse, error) {
	return typedPostInAccount[fwtypes.DatasetGetUploadEndpointResponse](ctx, r.client, "/datasets/"+pathEscape(datasetID)+":getUploadEndpoint", body, opts...)
}

func (r *DatasetsResource) UploadTyped(ctx context.Context, datasetID string, body any, opts ...RequestOption) (*fwtypes.DatasetUploadResponse, error) {
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
	path, err := typedAccountPath(r.client, "/datasets/"+pathEscape(datasetID)+":validateUpload", opts)
	if err != nil {
		return nil, err
	}
	err = r.client.Request(ctx, http.MethodPost, path, body, &out, opts...)
	return out, err
}

func (r *DeploymentsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPostInAccount[fwtypes.Deployment](ctx, r.client, "/deployments", body, opts...)
}

func (r *DeploymentsResource) UpdateTyped(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPatchInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID), body, opts...)
}

func (r *DeploymentsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DeploymentsPage, error) {
	return typedListInAccount[fwtypes.DeploymentsPage](ctx, r.client, "/deployments", query, opts...)
}

func (r *DeploymentsResource) GetTyped(ctx context.Context, deploymentID string, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedGetInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID), opts...)
}

func (r *DeploymentsResource) DeleteTyped(ctx context.Context, deploymentID string, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedDeleteInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID), opts...)
}

func (r *DeploymentsResource) ScaleTyped(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPatchInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID)+":scale", body, opts...)
}

func (r *DeploymentsResource) UndeleteTyped(ctx context.Context, deploymentID string, body any, opts ...RequestOption) (*fwtypes.Deployment, error) {
	return typedPostInAccount[fwtypes.Deployment](ctx, r.client, "/deployments/"+pathEscape(deploymentID)+":undelete", body, opts...)
}

func (r *DeploymentShapesResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DeploymentShapesPage, error) {
	return typedListInAccount[fwtypes.DeploymentShapesPage](ctx, r.client, "/deploymentShapes", query, opts...)
}

func (r *DeploymentShapesResource) GetTyped(ctx context.Context, shapeID string, opts ...RequestOption) (*fwtypes.DeploymentShape, error) {
	return typedGetInAccount[fwtypes.DeploymentShape](ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID), opts...)
}

func (r *DeploymentShapeVersionsResource) ListTyped(ctx context.Context, shapeID string, query any, opts ...RequestOption) (*fwtypes.DeploymentShapeVersionsPage, error) {
	return typedListInAccount[fwtypes.DeploymentShapeVersionsPage](ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID)+"/versions", query, opts...)
}

func (r *DeploymentShapeVersionsResource) GetTyped(ctx context.Context, shapeID, versionID string, opts ...RequestOption) (*fwtypes.DeploymentShapeVersion, error) {
	return typedGetInAccount[fwtypes.DeploymentShapeVersion](ctx, r.client, "/deploymentShapes/"+pathEscape(shapeID)+"/versions/"+pathEscape(versionID), opts...)
}

func (r *LoraResource) UpdateTyped(ctx context.Context, deployedModelID string, body any, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedPatchInAccount[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), body, opts...)
}

func (r *LoraResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.LoraPage, error) {
	return typedListInAccount[fwtypes.LoraPage](ctx, r.client, "/deployedModels", query, opts...)
}

func (r *LoraResource) GetTyped(ctx context.Context, deployedModelID string, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedGetInAccount[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), opts...)
}

func (r *LoraResource) LoadTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedPostInAccount[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels", body, opts...)
}

func (r *LoraResource) UnloadTyped(ctx context.Context, deployedModelID string, opts ...RequestOption) (*fwtypes.SharedDeployedModel, error) {
	return typedDeleteInAccount[fwtypes.SharedDeployedModel](ctx, r.client, "/deployedModels/"+pathEscape(deployedModelID), opts...)
}

func (r *BatchInferenceJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.BatchInferenceJob, error) {
	return typedPostInAccount[fwtypes.BatchInferenceJob](ctx, r.client, "/batchInferenceJobs", body, opts...)
}

func (r *BatchInferenceJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.BatchInferenceJobsPage, error) {
	return typedListInAccount[fwtypes.BatchInferenceJobsPage](ctx, r.client, "/batchInferenceJobs", query, opts...)
}

func (r *BatchInferenceJobsResource) GetTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.BatchInferenceJob, error) {
	return typedGetInAccount[fwtypes.BatchInferenceJob](ctx, r.client, "/batchInferenceJobs/"+pathEscape(jobID), opts...)
}

func (r *BatchInferenceJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.BatchInferenceJob, error) {
	return typedDeleteInAccount[fwtypes.BatchInferenceJob](ctx, r.client, "/batchInferenceJobs/"+pathEscape(jobID), opts...)
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

func (r *SecretsResource) GetTyped(ctx context.Context, secretID string, opts ...RequestOption) (*fwtypes.Secret, error) {
	return typedGetInAccount[fwtypes.Secret](ctx, r.client, "/secrets/"+pathEscape(secretID), opts...)
}

func (r *SecretsResource) DeleteTyped(ctx context.Context, secretID string, opts ...RequestOption) (*fwtypes.Secret, error) {
	return typedDeleteInAccount[fwtypes.Secret](ctx, r.client, "/secrets/"+pathEscape(secretID), opts...)
}

func (r *SupervisedFineTuningJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedPostInAccount[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs", body, opts...)
}

func (r *SupervisedFineTuningJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJobsPage, error) {
	return typedListInAccount[fwtypes.SupervisedFineTuningJobsPage](ctx, r.client, "/supervisedFineTuningJobs", query, opts...)
}

func (r *SupervisedFineTuningJobsResource) GetTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedGetInAccount[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *SupervisedFineTuningJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedDeleteInAccount[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *SupervisedFineTuningJobsResource) ResumeTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (*fwtypes.SupervisedFineTuningJob, error) {
	return typedPostInAccount[fwtypes.SupervisedFineTuningJob](ctx, r.client, "/supervisedFineTuningJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJobsPage, error) {
	return typedListInAccount[fwtypes.ReinforcementFineTuningJobsPage](ctx, r.client, "/reinforcementFineTuningJobs", query, opts...)
}

func (r *ReinforcementFineTuningJobsResource) GetTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedGetInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *ReinforcementFineTuningJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedDeleteInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID), opts...)
}

func (r *ReinforcementFineTuningJobsResource) CancelTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID)+":cancel", body, opts...)
}

func (r *ReinforcementFineTuningJobsResource) ResumeTyped(ctx context.Context, jobID string, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningJob, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningJob](ctx, r.client, "/reinforcementFineTuningJobs/"+pathEscape(jobID)+":resume", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStepsPage, error) {
	return typedListInAccount[fwtypes.ReinforcementFineTuningStepsPage](ctx, r.client, "/rlorTrainerJobs", query, opts...)
}

func (r *ReinforcementFineTuningStepsResource) GetTyped(ctx context.Context, trainerJobID string, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedGetInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID), opts...)
}

func (r *ReinforcementFineTuningStepsResource) DeleteTyped(ctx context.Context, trainerJobID string, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedDeleteInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID), opts...)
}

func (r *ReinforcementFineTuningStepsResource) ExecuteTyped(ctx context.Context, trainerJobID string, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID)+":executeTrainStep", body, opts...)
}

func (r *ReinforcementFineTuningStepsResource) ResumeTyped(ctx context.Context, trainerJobID string, body any, opts ...RequestOption) (*fwtypes.ReinforcementFineTuningStep, error) {
	return typedPostInAccount[fwtypes.ReinforcementFineTuningStep](ctx, r.client, "/rlorTrainerJobs/"+pathEscape(trainerJobID)+":resume", body, opts...)
}

func (r *DPOJobsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.DpoJob, error) {
	return typedPostInAccount[fwtypes.DpoJob](ctx, r.client, "/dpoJobs", body, opts...)
}

func (r *DPOJobsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.DPOJobsPage, error) {
	return typedListInAccount[fwtypes.DPOJobsPage](ctx, r.client, "/dpoJobs", query, opts...)
}

func (r *DPOJobsResource) GetTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.DpoJob, error) {
	return typedGetInAccount[fwtypes.DpoJob](ctx, r.client, "/dpoJobs/"+pathEscape(jobID), opts...)
}

func (r *DPOJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.DpoJob, error) {
	return typedDeleteInAccount[fwtypes.DpoJob](ctx, r.client, "/dpoJobs/"+pathEscape(jobID), opts...)
}

func (r *DPOJobsResource) GetMetricsFileEndpointTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.DPOJobGetMetricsFileEndpointResponse, error) {
	return typedGetInAccount[fwtypes.DPOJobGetMetricsFileEndpointResponse](ctx, r.client, "/dpoJobs/"+pathEscape(jobID)+":getMetricsFileEndpoint", withQuery(query, opts)...)
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

func (r *EvaluationJobsResource) GetTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.EvaluationJobGetResponse, error) {
	return typedGetInAccount[fwtypes.EvaluationJobGetResponse](ctx, r.client, "/evaluationJobs/"+pathEscape(jobID), opts...)
}

func (r *EvaluationJobsResource) DeleteTyped(ctx context.Context, jobID string, opts ...RequestOption) (*fwtypes.EvaluationJobGetResponse, error) {
	return typedDeleteInAccount[fwtypes.EvaluationJobGetResponse](ctx, r.client, "/evaluationJobs/"+pathEscape(jobID), opts...)
}

func (r *EvaluationJobsResource) GetLogEndpointTyped(ctx context.Context, jobID string, query any, opts ...RequestOption) (*fwtypes.EvaluationJobGetLogEndpointResponse, error) {
	return typedGetInAccount[fwtypes.EvaluationJobGetLogEndpointResponse](ctx, r.client, "/evaluationJobs/"+pathEscape(jobID)+":getExecutionLogEndpoint", withQuery(query, opts)...)
}

func (r *EvaluatorsResource) CreateTyped(ctx context.Context, body any, opts ...RequestOption) (*fwtypes.EvaluatorCreateResponse, error) {
	return typedPostInAccount[fwtypes.EvaluatorCreateResponse](ctx, r.client, "/evaluatorsV2", body, opts...)
}

func (r *EvaluatorsResource) UpdateTyped(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (*fwtypes.EvaluatorUpdateResponse, error) {
	return typedPatchInAccount[fwtypes.EvaluatorUpdateResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), body, opts...)
}

func (r *EvaluatorsResource) ListTyped(ctx context.Context, query any, opts ...RequestOption) (*fwtypes.EvaluatorsPage, error) {
	return typedListInAccount[fwtypes.EvaluatorsPage](ctx, r.client, "/evaluators", query, opts...)
}

func (r *EvaluatorsResource) GetTyped(ctx context.Context, evaluatorID string, opts ...RequestOption) (*fwtypes.EvaluatorGetResponse, error) {
	return typedGetInAccount[fwtypes.EvaluatorGetResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), opts...)
}

func (r *EvaluatorsResource) DeleteTyped(ctx context.Context, evaluatorID string, opts ...RequestOption) (*fwtypes.EvaluatorGetResponse, error) {
	return typedDeleteInAccount[fwtypes.EvaluatorGetResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID), opts...)
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

func (r *EvaluatorsResource) ValidateUploadTyped(ctx context.Context, evaluatorID string, body any, opts ...RequestOption) (*fwtypes.EvaluatorGetResponse, error) {
	return typedPostInAccount[fwtypes.EvaluatorGetResponse](ctx, r.client, "/evaluators/"+pathEscape(evaluatorID)+":validateUpload", body, opts...)
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
	path, err := typedAccountPath(client, suffix, opts)
	if err != nil {
		return nil, err
	}
	return typedPost[T](ctx, client, path, body, opts...)
}

func typedPatchInAccount[T any](ctx context.Context, client *Client, suffix string, body any, opts ...RequestOption) (*T, error) {
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
