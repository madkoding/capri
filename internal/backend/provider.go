package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// ProviderModels lista los modelos que ofrece un servidor OpenAI-compatible (GET /v1/models).
func ProviderModels(ctx context.Context, baseURL string) ([]string, error) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s respondió %s", baseURL, res.Status)
	}
	var d struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range d.Data {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out, nil
}
