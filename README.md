# Fireworks SDK Go

Unofficial Go port of the official Fireworks AI Python SDK.

This project targets Fireworks Python SDK `1.2.11` and keeps the Go
package version aligned exactly:

```go
fireworks.Version // "1.2.11"
```

Upstream references:

- Official Python SDK repo: https://github.com/fw-ai-external/python-sdk
- Fireworks REST API docs: https://docs.fireworks.ai/api-reference/introduction
- This Go port: https://github.com/ZaguanLabs/fireworks-sdk-go

## Status

The goal is API and behavior parity with the official Python SDK.

Current parity checks cover:

- generated resource method coverage
- generated type/export coverage
- request serialization, retries, errors, SSE streaming, file uploads, and
  account resolution
- FireTitan training lifecycle helpers and opt-in live contract tests

The latest local parity report is in
[`docs/resource-type-parity-1.2.11.md`](docs/resource-type-parity-1.2.11.md).
The broader training parity matrix is in
[`docs/parity-1.2.11.md`](docs/parity-1.2.11.md).

## Install

```sh
go get github.com/ZaguanLabs/fireworks-sdk-go
```

Use Go 1.22 or newer.

## Authentication

The client reads `FIREWORKS_API_KEY` by default:

```sh
export FIREWORKS_API_KEY="fw_..."
```

You can also pass the key explicitly:

```go
client, err := fireworks.NewClient(
	fireworks.WithAPIKey("fw_..."),
)
```

Do not hard-code API keys in source control.

## Account IDs

Many management endpoints are scoped to a Fireworks account. The Go client
follows the same resolution order as the Python SDK:

1. per-request `fireworks.WithAccountID("...")`
2. client-level `fireworks.WithDefaultAccountID("...")`
3. `FIREWORKS_ACCOUNT_ID`

Example:

```go
client, err := fireworks.NewClient(
	fireworks.WithDefaultAccountID("my-account"),
)
if err != nil {
	return err
}

models, err := client.Models.ListTyped(ctx, types.ModelListParams{})
```

Override the account for one request:

```go
models, err := client.Models.ListTyped(
	ctx,
	types.ModelListParams{},
	fireworks.WithAccountID("other-account"),
)
```

## Chat Completions

```go
package main

import (
	"context"
	"fmt"

	fireworks "github.com/ZaguanLabs/fireworks-sdk-go"
	"github.com/ZaguanLabs/fireworks-sdk-go/types"
)

func main() {
	ctx := context.Background()

	client, err := fireworks.NewClient()
	if err != nil {
		panic(err)
	}

	resp, err := client.Chat.Completions.CreateTyped(ctx, types.ChatCompletionCreateParams{
		Model: "accounts/fireworks/models/kimi-k2-instruct-0905",
		Messages: []types.SharedParamsChatMessage{
			{Role: "user", Content: "How do LLMs work?"},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Choices[0].Message.Content)
}
```

## Streaming

Streaming uses Server-Sent Events, matching the Python SDK behavior.

```go
stream, err := client.Chat.Completions.CreateTypedStream(ctx, types.ChatCompletionCreateParams{
	Model: "accounts/fireworks/models/kimi-k2-instruct-0905",
	Messages: []types.SharedParamsChatMessage{
		{Role: "user", Content: "How do LLMs work?"},
	},
})
if err != nil {
	return err
}
defer stream.Close()

for stream.Next() {
	chunk := stream.Current()
	if len(chunk.Choices) > 0 {
		fmt.Print(chunk.Choices[0].Delta.Content)
	}
}
if err := stream.Err(); err != nil {
	return err
}
```

The generic stream APIs are also available:

```go
stream, err := client.Chat.Completions.CreateStream(ctx, map[string]any{
	"model": "accounts/fireworks/models/kimi-k2-instruct-0905",
	"messages": []map[string]string{
		{"role": "user", "content": "How do LLMs work?"},
	},
})
```

## Typed And Generic APIs

Most resources expose both generic `map[string]any` methods and typed methods:

```go
// Generic response.
raw, err := client.Models.Get(ctx, "my-model")

// Typed response.
model, err := client.Models.GetTyped(ctx, "my-model", types.ModelGetParams{})
```

Typed request and response structs live in the `types` package. They are
generated from the Python SDK type catalog and include Python-compatible JSON
field aliases.

## Pagination

List responses expose page helper methods:

```go
page, err := client.Models.ListTyped(ctx, types.ModelListParams{
	PageSize: 50,
})
if err != nil {
	return err
}

for _, model := range page.Models {
	fmt.Println(model.Name)
}

if page.HasNextPage() {
	next := page.NextPageInfo()
	page, err = client.Models.ListTyped(ctx, types.ModelListParams{}, fireworks.WithQuery(next.Params))
}
```

## Dataset Uploads

Small dataset files can be uploaded directly:

```go
file, err := fireworks.NewFileFromPath("train.jsonl")
if err != nil {
	return err
}

upload, err := client.Datasets.UploadFileTyped(ctx, "my-dataset", file)
if err != nil {
	return err
}

fmt.Println(upload)
```

For larger files, use the signed upload endpoint:

```go
endpoint, err := client.Datasets.GetUploadEndpointTyped(ctx, "my-dataset", map[string]any{})
```

## Raw And Undocumented Requests

For endpoints that are not yet represented by a generated resource method:

```go
resp, err := client.Raw(ctx, "POST", "/v1/custom/path", map[string]any{
	"my_param": true,
})
if err != nil {
	return err
}
defer resp.Body.Close()
```

There are also convenience helpers such as `Get`, `Post`, `RequestRaw`,
`RequestBytesRaw`, and `RequestReaderRaw`.

## Error Handling, Timeouts, And Retries

The client mirrors Python SDK behavior for:

- API status errors
- request metadata on errors
- response validation errors
- timeout and connection error classification
- `Retry-After`, `retry-after-ms`, and `X-Should-Retry` retry handling

Client defaults:

- timeout: `fireworks.DefaultTimeout`
- connect timeout: `fireworks.DefaultConnectTimeout`
- max retries: `fireworks.DefaultMaxRetries`

Configure defaults:

```go
client, err := fireworks.NewClient(
	fireworks.WithDefaultTimeout(2*time.Minute),
	fireworks.WithMaxRetries(4),
)
```

Configure one request:

```go
resp, err := client.Models.ListTyped(
	ctx,
	types.ModelListParams{},
	fireworks.WithTimeout(10*time.Second),
	fireworks.WithRequestMaxRetries(0),
)
```

## FireTitan Training SDK

The Go port includes the Fireworks training infrastructure layer from
`fireworks.training.sdk`: trainer jobs, deployments, hotload, sampler clients,
managed provisioning, checkpoint promotion, and setup helpers.

Training APIs require a training-scoped Fireworks API key. Inference-only keys
may work for completions and deployment inference, but Fireworks returns HTTP
401 for trainer lifecycle and training-shape endpoints.

See [`docs/training-sdk.md`](docs/training-sdk.md) for Go examples ported from
the official Python training docs.

The setup helper CLI mirrors the Python SDK scripts:

```sh
go run ./cmd/fireworks-training setup-trainer \
  --display-name ablation-eager \
  --extra-args "--forward-only --no-compile" \
  --custom-image-tag your-image-tag

go run ./cmd/fireworks-training setup-deployment \
  --deployment-id verify-ablation \
  --deployment-shape accounts/ACCOUNT/deploymentShapes/YOUR-SHAPE
```

## Live Contract Tests

Live tests are opt-in:

```sh
FIREWORKS_SDK_GO_LIVE=1 \
FIREWORKS_API_KEY=fw_... \
go test ./training/sdk -run Live -count=1
```

With only `FIREWORKS_API_KEY`, this runs a read-only account-list smoke test.
Resource-specific trainer/deployment/checkpoint tests require additional
environment variables. See
[`docs/live-contract-tests.md`](docs/live-contract-tests.md).

## Regenerating Types

The Python SDK source snapshot is intentionally ignored by git. To regenerate
types, provide a local copy of the official Python SDK source:

```sh
FIREWORKS_PY_TYPES_ROOT=/path/to/python-sdk-1.2.11/src/fireworks/types \
  go generate ./types
```

The default generator path is:

```text
docs/fireworks-py/python-sdk-1.2.11/src/fireworks/types
```

## Development

Run the unit suite:

```sh
go test ./...
```

Run the parity report:

```sh
python scripts/report_parity.py
```

If your sandbox prevents local `httptest` listeners, run tests in an
environment that allows loopback sockets.

## Releases

Releases are created from git tags by
[`Release`](.github/workflows/release.yml).

Use Go module semver tags:

```sh
git tag v1.2.11
git push origin v1.2.11
```

The workflow checks that `version.go` matches the tag, runs `go test ./...`,
and creates or updates the GitHub Release. Release notes include a GitHub
compare changelog link between the previous release and the new tag.

Tag push defaults:

- tags with a prerelease suffix, such as `v1.3.0-alpha.1`, create a
  prerelease and do not become Latest
- stable tags, such as `v1.2.0`, create a normal release and become Latest

To override those defaults, run the workflow manually with an existing tag and
choose `prerelease` and `make_latest` values explicitly.

## Documentation Port Notes

The upstream Python repo currently has these documentation entry points:

- `README.md`: general SDK usage, account IDs, streaming, pagination, uploads,
  errors, retries, raw requests, and versioning.
- `api.md`: generated Python API index.
- `src/fireworks/training/README.md`: training package overview.
- `src/fireworks/training/sdk/README.md`: training infrastructure details.

This repo ports the general README content into Go form here, and ports the
training docs into [`docs/training-sdk.md`](docs/training-sdk.md). The generated
Python `api.md` is not copied verbatim because the Go resource/type surface is
already checked by `scripts/report_parity.py` and documented in the checked-in
parity reports.
