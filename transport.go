package greenflags

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type flagsEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Data    struct {
		Flags []Flag `json:"flags"`
	} `json:"data"`
}

func requestFlags(ctx context.Context, httpClient *http.Client, url, apiToken string) (map[string]Flag, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(url), "/") + "/v1/flags"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &Error{Code: "NETWORK_ERROR", Message: err.Error(), Status: 0}
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &Error{Code: "NETWORK_ERROR", Message: err.Error(), Status: 0}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{Code: "NETWORK_ERROR", Message: err.Error(), Status: resp.StatusCode}
	}

	var envelope flagsEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, &Error{Code: "PARSE_ERROR", Message: "Invalid response from server", Status: resp.StatusCode}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := envelope.Error
		if code == "" {
			code = "REQUEST_ERROR"
		}
		message := envelope.Message
		if message == "" {
			message = resp.Status
		}
		return nil, &Error{Code: code, Message: message, Status: resp.StatusCode}
	}

	if !envelope.Success || envelope.Data.Flags == nil {
		return nil, &Error{Code: "PARSE_ERROR", Message: "Invalid response from server", Status: resp.StatusCode}
	}

	result := make(map[string]Flag, len(envelope.Data.Flags))
	for _, flag := range envelope.Data.Flags {
		result[flag.Key] = flag
	}
	return result, nil
}
