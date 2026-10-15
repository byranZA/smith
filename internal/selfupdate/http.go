package selfupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// HTTP is the Getter that reaches the network: it fetches over its Client.
type HTTP struct {
	// Client is the HTTP client requests travel over.
	Client *http.Client
}

// Get fetches url and returns its body. A response whose status is not 200 OK
// is an error naming the status.
func (h HTTP) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // the body is read in full, and a close failure changes nothing about it.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}
