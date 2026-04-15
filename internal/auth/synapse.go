package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SynapseClient verifies Matrix access tokens against a Synapse homeserver.
type SynapseClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSynapseClient creates a client pointing at the given Synapse base URL.
func NewSynapseClient(synapseURL string) *SynapseClient {
	return &SynapseClient{
		baseURL: synapseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// VerifyToken checks a Matrix access token and returns the associated user ID.
func (c *SynapseClient) VerifyToken(ctx context.Context, matrixToken string) (string, error) {
	url := c.baseURL + "/_matrix/client/r0/account/whoami"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+matrixToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("contacting synapse: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("synapse returned status %d", resp.StatusCode)
	}

	var result struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if result.UserID == "" {
		return "", fmt.Errorf("empty user_id in response")
	}

	return result.UserID, nil
}
