// Package github : client minimal de l'API REST GitHub (net/http) pour lire un
// fichier, créer une branche, committer, ouvrir une PR, créer un repo. Auth par
// token Bearer. Aucune dépendance externe (l'app reste stateless, image sans git).
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.github.com"

// Client appelle l'API GitHub pour un owner (org) donné.
type Client struct {
	owner string
	token string
	http  *http.Client
}

func New(owner, token string) *Client {
	return &Client{owner: owner, token: token, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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
		return fmt.Errorf("github %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("github: réponse illisible: %w", err)
		}
	}
	return nil
}

// GetFile lit un fichier (content décodé + son blob sha) sur une ref.
func (c *Client) GetFile(ctx context.Context, repo, path, ref string) (content []byte, sha string, err error) {
	var r struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		SHA      string `json:"sha"`
	}
	p := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", c.owner, repo, path, ref)
	if err := c.do(ctx, http.MethodGet, p, nil, &r); err != nil {
		return nil, "", err
	}
	if r.Encoding != "base64" {
		return nil, "", fmt.Errorf("github: encodage inattendu %q", r.Encoding)
	}
	dec, err := base64.StdEncoding.DecodeString(cleanB64(r.Content))
	if err != nil {
		return nil, "", fmt.Errorf("github: décodage base64: %w", err)
	}
	return dec, r.SHA, nil
}

func cleanB64(s string) string {
	// L'API insère des sauts de ligne dans le base64.
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' && s[i] != '\r' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// BranchSHA renvoie le SHA du commit de tête d'une branche.
func (c *Client) BranchSHA(ctx context.Context, repo, branch string) (string, error) {
	var r struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	p := fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", c.owner, repo, branch)
	if err := c.do(ctx, http.MethodGet, p, nil, &r); err != nil {
		return "", err
	}
	return r.Object.SHA, nil
}

// CreateBranch crée refs/heads/<branch> pointant sur fromSHA.
func (c *Client) CreateBranch(ctx context.Context, repo, branch, fromSHA string) error {
	body := map[string]string{"ref": "refs/heads/" + branch, "sha": fromSHA}
	p := fmt.Sprintf("/repos/%s/%s/git/refs", c.owner, repo)
	return c.do(ctx, http.MethodPost, p, body, nil)
}

// PutFile crée ou met à jour un fichier sur une branche (sha vide = création).
func (c *Client) PutFile(ctx context.Context, repo, path, branch string, content []byte, sha, message string) error {
	body := map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	}
	if sha != "" {
		body["sha"] = sha
	}
	p := fmt.Sprintf("/repos/%s/%s/contents/%s", c.owner, repo, path)
	return c.do(ctx, http.MethodPut, p, body, nil)
}

// PR décrit une pull request ouverte.
type PR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Merged  bool   `json:"merged"`
	State   string `json:"state"`
}

// CreatePR ouvre une PR head->base et renvoie son numéro + URL.
func (c *Client) CreatePR(ctx context.Context, repo, head, base, title, body string) (PR, error) {
	var pr PR
	req := map[string]string{"title": title, "head": head, "base": base, "body": body}
	p := fmt.Sprintf("/repos/%s/%s/pulls", c.owner, repo)
	if err := c.do(ctx, http.MethodPost, p, req, &pr); err != nil {
		return PR{}, err
	}
	return pr, nil
}

// GetPR renvoie l'état d'une PR (merged/state).
func (c *Client) GetPR(ctx context.Context, repo string, number int) (PR, error) {
	var pr PR
	p := fmt.Sprintf("/repos/%s/%s/pulls/%d", c.owner, repo, number)
	if err := c.do(ctx, http.MethodGet, p, nil, &pr); err != nil {
		return PR{}, err
	}
	return pr, nil
}

// Repo décrit un repo créé.
type Repo struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

// CreateOrgRepo crée un repo privé dans l'org (auto_init pour avoir une branche
// par défaut). Renvoie l'URL et la branche par défaut effective.
func (c *Client) CreateOrgRepo(ctx context.Context, name, description string) (Repo, error) {
	var r Repo
	body := map[string]any{
		"name":        name,
		"private":     true,
		"description": description,
		"auto_init":   true,
	}
	p := fmt.Sprintf("/orgs/%s/repos", c.owner)
	if err := c.do(ctx, http.MethodPost, p, body, &r); err != nil {
		return Repo{}, err
	}
	return r, nil
}

// RepoExists indique si un repo existe déjà dans l'org.
func (c *Client) RepoExists(ctx context.Context, name string) (bool, error) {
	p := fmt.Sprintf("/repos/%s/%s", c.owner, name)
	err := c.do(ctx, http.MethodGet, p, nil, nil)
	if err == nil {
		return true, nil
	}
	// Le message d'erreur contient le code HTTP ; 404 = absent.
	if bytes.Contains([]byte(err.Error()), []byte("HTTP 404")) {
		return false, nil
	}
	return false, err
}
