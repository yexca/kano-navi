package llmadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Outbound provider requests share these rules: the destination is the
// operator's validated base URL, the deadline is explicit, redirects are
// refused, the body is size-capped, and every failure is reduced to a fixed
// message so an upstream body or credential never reaches a response or log.

const (
	messageListTimeout    = "model list request timed out"
	messageListFailed     = "model list request failed"
	messageRequestTimeout = "provider request timed out"
	messageRequestFailed  = "provider request failed"
)

var (
	errRedirect       = errors.New("provider redirect refused")
	errProviderStatus = errors.New("provider returned an error status")
	errTooLarge       = errors.New("provider response too large")
	errPayload        = errors.New("provider response is not usable")
)

func refuseRedirect(*http.Request, []*http.Request) error { return errRedirect }

func providerHeaders(protocol, apiKey string) http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	if protocol == protocolAnthropic {
		headers.Set("X-Api-Key", apiKey)
		headers.Set("Anthropic-Version", "2023-06-01")
	} else {
		headers.Set("Authorization", "Bearer "+apiKey)
	}
	return headers
}

func outputTokenLimit(protocol string) string {
	switch protocol {
	case protocolAnthropic:
		return "max_tokens"
	case protocolChat:
		return "max_completion_tokens"
	default:
		return "max_output_tokens"
	}
}

// requestJSON performs one bounded request and decodes a JSON body of at
// most limit bytes, counted on the bytes actually read.
func (m *Module) requestJSON(ctx context.Context, method, address string, headers http.Header, body []byte, limit int64) (any, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return nil, errPayload
	}
	request.Header = headers
	response, err := m.client.Do(request)
	if err != nil {
		// The transport error can embed the request URL; keep it internal.
		return nil, errPayload
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, errProviderStatus
	}
	if response.ContentLength > limit {
		return nil, errTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errPayload
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	var payload any
	if json.Unmarshal(data, &payload) != nil {
		return nil, errPayload
	}
	return payload, nil
}

func (m *Module) fetchModelPages(ctx context.Context, provider *Provider, apiKey string) ([]RemoteModel, error) {
	endpoint, err := url.Parse(modelsEndpoint(provider.BaseURL))
	if err != nil || endpoint.Host == "" {
		return nil, errPayload
	}
	anthropic := provider.Protocol == protocolAnthropic
	headers := providerHeaders(provider.Protocol, apiKey)
	headers.Set("Accept", "application/json")
	collected := []RemoteModel{}
	cursors := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ {
		if anthropic {
			query := url.Values{"limit": {"100"}}
			if cursor != "" {
				query.Set("after_id", cursor)
			}
			endpoint.RawQuery = query.Encode()
		}
		payload, requestErr := m.requestJSON(ctx, http.MethodGet, endpoint.String(), headers.Clone(), nil, maxModelListBytes)
		if requestErr != nil {
			return nil, requestErr
		}
		collected = append(collected, parseModelList(payload)...)
		if !anthropic {
			return finishModelList(collected), nil
		}
		if payload == nil {
			return nil, errPayload
		}
		object, _ := payload.(map[string]any)
		if !jsTruthy(object["has_more"]) {
			return finishModelList(collected), nil
		}
		cursor = truthyString(object["last_id"])
		if !modelIDPattern.MatchString(cursor) || cursors[cursor] {
			return nil, errPayload
		}
		cursors[cursor] = true
	}
	return nil, errPayload
}

// fetchRemoteModels lists a provider's models within one overall deadline.
// The returned error text is one of the two fixed model-list messages.
func (m *Module) fetchRemoteModels(ctx context.Context, provider *Provider, apiKey string) ([]RemoteModel, error) {
	timeout := provider.TimeoutMs
	if timeout == 0 {
		timeout = 15000
	}
	timeout = min(60000, max(1000, timeout))
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	models, err := m.fetchModelPages(ctx, provider, apiKey)
	if err == nil {
		return models, nil
	}
	if ctx.Err() != nil {
		return nil, errors.New(messageListTimeout)
	}
	return nil, errors.New(messageListFailed)
}

// probeInference sends the cheapest possible completion to prove that the
// credential, endpoint and model work together.
func (m *Module) probeInference(ctx context.Context, provider *Provider, apiKey, model string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(provider.TimeoutMs)*time.Millisecond)
	defer cancel()
	body := map[string]any{"model": model, outputTokenLimit(provider.Protocol): 1}
	if provider.Protocol == protocolResponses {
		body["store"] = false
		body["input"] = "ping"
	} else {
		body["messages"] = []map[string]string{{"role": "user", "content": "ping"}}
	}
	encoded, err := json.Marshal(body)
	if err == nil {
		_, err = m.requestJSON(ctx, http.MethodPost, inferenceEndpoint(provider.BaseURL, provider.Protocol),
			providerHeaders(provider.Protocol, apiKey), encoded, maxInferenceBytes)
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return errors.New(messageRequestTimeout)
	}
	return errors.New(messageRequestFailed)
}
