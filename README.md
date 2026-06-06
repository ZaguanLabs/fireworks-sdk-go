# fireworks-sdk-go
Unofficial Fireworks SDK in Go ported from their Python SDK

This port currently targets the official Python SDK version `1.2.0-alpha.76`.

```go
import (
	fireworks "github.com/ZaguanLabs/fireworks-sdk-go"
	"github.com/ZaguanLabs/fireworks-sdk-go/types"
)

client, err := fireworks.NewClient(
	fireworks.WithAPIKey("fw_..."),
	fireworks.WithDefaultAccountID("my-account"),
)
if err != nil {
	return err
}

resp, err := client.Chat.Completions.CreateTyped(ctx, types.ChatCompletionCreateParams{
	Model: "accounts/fireworks/models/kimi-k2-instruct-0905",
	Messages: []types.SharedParamsChatMessage{
		{Role: "user", Content: "Hello"},
	},
})
```

The Python SDK type catalog is mirrored in the `types` subpackage. Regeneration
expects the ignored Python SDK source snapshot at
`docs/fireworks-py/python-sdk-1.2.0-alpha.76/src/fireworks/types`:

```sh
go generate ./types
```

You can also point the generator at another local checkout or extracted source
tree:

```sh
FIREWORKS_PY_TYPES_ROOT=/path/to/python-sdk-1.2.0-alpha.76/src/fireworks/types go generate ./types
```

Training setup helpers equivalent to the Python SDK scripts are available
through the `fireworks-training` command:

```sh
go run ./cmd/fireworks-training setup-trainer \
  --display-name ablation-eager \
  --extra-args "--forward-only --no-compile" \
  --custom-image-tag your-image-tag

go run ./cmd/fireworks-training setup-deployment \
  --deployment-id verify-ablation \
  --deployment-shape accounts/ACCOUNT/deploymentShapes/YOUR-SHAPE
```
