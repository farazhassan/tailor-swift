package jd

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// maxFetchBytes caps a fetched JD body; real postings are far smaller.
const maxFetchBytes = 5 << 20 // 5 MiB

const userAgent = "tailor-swift/0.1 (+https://github.com/farazhassan/tailor-swift)"

// Fetch GETs url and returns the response body as a string. A nil client uses
// http.DefaultClient. Non-2xx responses are errors.
func Fetch(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("jd: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jd: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("jd: fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", fmt.Errorf("jd: read body: %w", err)
	}
	return string(body), nil
}
