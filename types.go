package fireworks

import (
	"net/http"
	"net/url"
	"time"
)

type JSON map[string]any

type Response = JSON

type RequestOptions struct {
	AccountID string
	Headers   http.Header
	Query     url.Values
	ExtraBody map[string]any
	Timeout   time.Duration
}

type RequestOption func(*RequestOptions)

func WithAccountID(accountID string) RequestOption {
	return func(o *RequestOptions) {
		o.AccountID = accountID
	}
}

func WithHeader(key, value string) RequestOption {
	return func(o *RequestOptions) {
		if o.Headers == nil {
			o.Headers = make(http.Header)
		}
		o.Headers.Set(key, value)
	}
}

func WithHeaders(headers map[string]string) RequestOption {
	return func(o *RequestOptions) {
		if o.Headers == nil {
			o.Headers = make(http.Header)
		}
		for key, value := range headers {
			o.Headers.Set(key, value)
		}
	}
}

func WithQueryParam(key string, value any) RequestOption {
	return func(o *RequestOptions) {
		if o.Query == nil {
			o.Query = make(url.Values)
		}
		addQueryValue(o.Query, key, value)
	}
}

func WithQuery(query map[string]any) RequestOption {
	return func(o *RequestOptions) {
		if o.Query == nil {
			o.Query = make(url.Values)
		}
		for key, value := range query {
			addQueryValue(o.Query, key, value)
		}
	}
}

func WithExtraBodyField(key string, value any) RequestOption {
	return func(o *RequestOptions) {
		if o.ExtraBody == nil {
			o.ExtraBody = make(map[string]any)
		}
		o.ExtraBody[key] = value
	}
}

func WithExtraBody(body map[string]any) RequestOption {
	return func(o *RequestOptions) {
		if o.ExtraBody == nil {
			o.ExtraBody = make(map[string]any)
		}
		for key, value := range body {
			o.ExtraBody[key] = value
		}
	}
}

func WithTimeout(timeout time.Duration) RequestOption {
	return func(o *RequestOptions) {
		o.Timeout = timeout
	}
}

func applyRequestOptions(opts []RequestOption) RequestOptions {
	var out RequestOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}
