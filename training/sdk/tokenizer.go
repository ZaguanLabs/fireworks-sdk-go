package sdk

import (
	"fmt"
	"strings"
)

const TokenizerModelRequiredMessage = "get_tokenizer() requires a tokenizer_model. FireTitan does not resolve tokenizers server-side; pass tokenizer_model to from_firetitan_config()/install_tinker_service_client()."

func ResolveTokenizerModel(tokenizerModel string) (string, error) {
	tokenizerModel = strings.TrimSpace(tokenizerModel)
	if tokenizerModel == "" {
		return "", fmt.Errorf("%s", TokenizerModelRequiredMessage)
	}
	return tokenizerModel, nil
}
