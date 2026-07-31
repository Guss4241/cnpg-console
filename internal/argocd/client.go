// Package argocd : client minimal de l'API REST ArgoCD (net/http) pour
// rafraîchir/synchroniser une Application et lire son statut. Auth par token
// Bearer (compte local ArgoCD avec apiKey). Aucune dépendance externe.
package argocd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client pointe vers un serveur ArgoCD (ex. argo-cd.consoneo.tech).
type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(server, token string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in par config
	}
	return &Client{
		base:  "https://" + server,
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("argocd %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("argocd: réponse illisible: %w", err)
		}
	}
	return nil
}

// AppStatus résume l'état de synchro/santé d'une Application.
type AppStatus struct {
	Sync    string `json:"sync"`
	Health  string `json:"health"`
	Message string `json:"message"`
}

type appResponse struct {
	Status struct {
		Sync struct {
			Status string `json:"status"`
		} `json:"sync"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		OperationState struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		} `json:"operationState"`
	} `json:"status"`
}

// Get renvoie l'état courant d'une Application. refresh peut valoir "", "normal"
// ou "hard" pour forcer un rafraîchissement du cache repo-server.
func (c *Client) Get(ctx context.Context, name, refresh string) (AppStatus, error) {
	p := "/api/v1/applications/" + name
	if refresh != "" {
		p += "?refresh=" + refresh
	}
	var r appResponse
	if err := c.do(ctx, http.MethodGet, p, nil, &r); err != nil {
		return AppStatus{}, err
	}
	return AppStatus{
		Sync:    r.Status.Sync.Status,
		Health:  r.Status.Health.Status,
		Message: r.Status.OperationState.Message,
	}, nil
}

// Sync déclenche une synchronisation (ServerSideApply activé). Si prune est vrai,
// les ressources orphelines (manifest supprimé du git) sont supprimées du cluster.
// Best-effort : renvoie le statut après déclenchement.
func (c *Client) Sync(ctx context.Context, name string, prune bool) (AppStatus, error) {
	body := map[string]any{
		"syncOptions": map[string]any{"items": []string{"ServerSideApply=true"}},
	}
	if prune {
		body["prune"] = true
	}
	p := "/api/v1/applications/" + name + "/sync"
	var r appResponse
	if err := c.do(ctx, http.MethodPost, p, body, &r); err != nil {
		return AppStatus{}, err
	}
	return AppStatus{
		Sync:    r.Status.Sync.Status,
		Health:  r.Status.Health.Status,
		Message: r.Status.OperationState.Message,
	}, nil
}
