package subscriptions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const fetchTimeout = 15 * time.Second
const maxFetchRedirects = 5
const maxListBytes = 16 << 20

func FetchList(rawURL string) (string, error) {
	return FetchListContext(context.Background(), rawURL)
}

func FetchListContext(ctx context.Context, rawURL string) (string, error) {
	return FetchListWithInterface(ctx, rawURL, "")
}

// FetchListWithInterface retries the original URL via the system route if the
// preferred interface fails. A failed attempt never returns a partial list.
func FetchListWithInterface(ctx context.Context, rawURL, interfaceName string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid url")
	}
	if !isSupportedFetchURL(parsed) {
		return "", fmt.Errorf("unsupported url scheme")
	}
	if err := ValidateFetchInterface(interfaceName); err != nil {
		return "", err
	}
	return fetchWithFallback(ctx, interfaceName, func(ctx context.Context, name string) (string, error) {
		return fetchListAttempt(ctx, rawURL, name)
	})
}

func fetchListAttempt(ctx context.Context, rawURL, interfaceName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	transport, err := fetchTransport(interfaceName)
	if err != nil {
		return "", err
	}
	defer transport.CloseIdleConnections()
	parsed, _ := url.Parse(rawURL) // Validated before either attempt.

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	visited := map[string]struct{}{parsed.String(): {}}

	for redirects := 0; ; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
			location, err := resp.Location()
			resp.Body.Close()
			if err != nil {
				return "", fmt.Errorf("bad redirect location: %w", err)
			}
			if !location.IsAbs() {
				location = parsed.ResolveReference(location)
			}
			if !isSupportedFetchURL(location) {
				return "", fmt.Errorf("unsupported redirect url")
			}
			if _, exists := visited[location.String()]; exists {
				return "", fmt.Errorf("redirect loop detected")
			}
			if redirects >= maxFetchRedirects {
				return "", fmt.Errorf("too many redirects")
			}
			visited[location.String()] = struct{}{}
			parsed = location
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("bad response status: %d", resp.StatusCode)
		}

		if resp.ContentLength > maxListBytes {
			return "", fmt.Errorf("subscription exceeds 16 MiB limit")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxListBytes+1))
		if err != nil {
			return "", err
		}
		if len(data) > maxListBytes {
			return "", fmt.Errorf("subscription exceeds 16 MiB limit")
		}
		return string(data), nil
	}
}

func isSupportedFetchURL(parsed *url.URL) bool {
	return parsed != nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
