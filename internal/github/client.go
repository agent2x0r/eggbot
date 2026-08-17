package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultAPI   = "https://api.github.com"
	maxBody      = 256 << 10
	maxExcerpt   = 1200
	defaultAgent = "eggbot"
)

var changelogFiles = []string{
	"CHANGELOG.md", "CHANGELOG", "CHANGELOG.txt",
	"changelog.md", "CHANGES.md", "NEWS.md", "HISTORY.md",
}

type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

type Info struct {
	Owner, Name   string
	URL           string
	ReleaseTag    string
	ReleaseName   string
	ReleaseDate   string
	ReleaseURL    string
	ReleaseNotes  string
	ChangelogURL  string
	ChangelogText string
}

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = defaultAPI
	}
	return &Client{
		BaseURL:   baseURL,
		Token:     token,
		UserAgent: defaultAgent,
		HTTP:      &http.Client{Timeout: timeout},
	}
}

func (c *Client) Lookup(ctx context.Context, spec string) (Info, error) {
	owner, repo, ok := Parse(spec)
	if !ok {
		return Info{}, fmt.Errorf("not a GitHub repository")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	info := Info{Owner: owner, Name: repo, URL: PageURL(owner, repo)}
	repoJSON, err := c.get(ctx, "/repos/"+owner+"/"+repo)
	if err != nil {
		return Info{}, err
	}
	var repoOut struct {
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(repoJSON, &repoOut); err != nil {
		return Info{}, fmt.Errorf("github repo decode: %w", err)
	}
	if repoOut.HTMLURL != "" {
		info.URL = repoOut.HTMLURL
	}
	if relJSON, err := c.get(ctx, "/repos/"+owner+"/"+repo+"/releases/latest"); err == nil {
		var rel struct {
			TagName     string `json:"tag_name"`
			Name        string `json:"name"`
			PublishedAt string `json:"published_at"`
			HTMLURL     string `json:"html_url"`
			Body        string `json:"body"`
		}
		if json.Unmarshal(relJSON, &rel) == nil {
			info.ReleaseTag = rel.TagName
			info.ReleaseName = rel.Name
			if i := strings.IndexByte(rel.PublishedAt, 'T'); i > 0 {
				info.ReleaseDate = rel.PublishedAt[:i]
			} else {
				info.ReleaseDate = rel.PublishedAt
			}
			info.ReleaseURL = rel.HTMLURL
			info.ReleaseNotes = excerpt(rel.Body, maxExcerpt)
		}
	}
	for _, name := range changelogFiles {
		fileJSON, err := c.get(ctx, "/repos/"+owner+"/"+repo+"/contents/"+name)
		if err != nil {
			continue
		}
		var file struct {
			HTMLURL  string `json:"html_url"`
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
			Size     int    `json:"size"`
		}
		if json.Unmarshal(fileJSON, &file) != nil {
			continue
		}
		info.ChangelogURL = file.HTMLURL
		if file.Encoding == "base64" && file.Size <= maxBody {
			raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
			if err == nil {
				info.ChangelogText = excerpt(string(raw), maxExcerpt)
			}
		}
		break
	}
	return info, nil
}

func (i Info) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "repo: %s\nurl: %s\n", Canonical(i.Owner, i.Name), i.URL)
	if i.ReleaseTag != "" {
		fmt.Fprintf(&b, "latest_release: %s", i.ReleaseTag)
		if i.ReleaseDate != "" {
			fmt.Fprintf(&b, " (%s)", i.ReleaseDate)
		}
		b.WriteByte('\n')
		if i.ReleaseURL != "" {
			fmt.Fprintf(&b, "release_url: %s\n", i.ReleaseURL)
		}
		if i.ReleaseNotes != "" {
			fmt.Fprintf(&b, "release_notes: %s\n", oneLine(i.ReleaseNotes))
		}
	} else {
		b.WriteString("latest_release: none published\n")
	}
	if i.ChangelogURL != "" {
		fmt.Fprintf(&b, "changelog_url: %s\n", i.ChangelogURL)
	}
	if i.ChangelogText != "" {
		fmt.Fprintf(&b, "changelog: %s\n", oneLine(i.ChangelogText))
	}
	return strings.TrimSpace(b.String())
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = defaultAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBody {
		raw = raw[:maxBody]
	}
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("github: not found")
	}
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		return nil, fmt.Errorf("github: rate limited")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github http %d", resp.StatusCode)
	}
	return raw, nil
}

func excerpt(s string, n int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSpace(s)
	if s == "" || n <= 0 || len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if i := strings.LastIndexAny(s[:cut], "\n."); i > n/2 {
		cut = i
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
