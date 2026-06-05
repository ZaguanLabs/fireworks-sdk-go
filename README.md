# fireworks-sdk-go
Unofficial Fireworks SDK in Go ported from their Python SDK

This port currently targets the official Python SDK version `1.2.0-alpha.75`.

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

The Python SDK type catalog is mirrored in the `types` subpackage and can be
regenerated from the vendored Python SDK source:

```sh
go generate ./types
```
