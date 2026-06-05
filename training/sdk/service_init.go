package sdk

type FiretitanServiceClientInitOptions struct {
	APIKey         string
	DefaultHeaders map[string]string
	ClientConfig   map[string]bool
}

func NewFiretitanServiceClientInitOptions(apiKey string, defaultHeaders map[string]string) FiretitanServiceClientInitOptions {
	headers := cloneStringMap(defaultHeaders)
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["X-API-Key"]; !ok && apiKey != "" {
		headers["X-API-Key"] = apiKey
	}
	return FiretitanServiceClientInitOptions{
		APIKey:         apiKey,
		DefaultHeaders: headers,
		ClientConfig:   CloneFiretitanTinkerClientConfig(),
	}
}
