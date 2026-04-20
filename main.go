package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

type ghIssue struct {
	Title       string    `json:"title"`
	HTMLURL     string    `json:"html_url"`
	CreatedAt   time.Time `json:"created_at"`
	PullRequest *struct{} `json:"pull_request"` // non-nil when the issue is actually a PR
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

type prEntry struct {
	Title string
	URL   string
	Repo  string
	Age   string
}

var (
	githubToken string
	repoList    []string
	mu          sync.Mutex
	cached      []prEntry
	cacheTime   time.Time
)

func fmtAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func fetchIssues() ([]prEntry, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var entries []prEntry

	for _, repo := range repoList {
		url := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=open&creator=renovate%%5Bbot%%5D&per_page=100", repo)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+githubToken)
		req.Header.Set("Accept", "application/vnd.github.v3+json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var issues []ghIssue
		if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
			return nil, err
		}

		shortName := repo
		if i := strings.Index(repo, "/"); i >= 0 {
			shortName = repo[i+1:]
		}

		for _, issue := range issues {
			if issue.PullRequest != nil {
				continue // issues endpoint returns PRs too
			}
			entries = append(entries, prEntry{
				Title: issue.Title,
				URL:   issue.HTMLURL,
				Repo:  shortName,
				Age:   fmtAge(issue.CreatedAt),
			})
		}
	}
	return entries, nil
}

func getIssues() ([]prEntry, error) {
	mu.Lock()
	defer mu.Unlock()
	if time.Since(cacheTime) < cacheTTL {
		return cached, nil
	}
	issues, err := fetchIssues()
	if err != nil {
		return cached, err // return stale on error
	}
	cached = issues
	cacheTime = time.Now()
	return cached, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	issues, err := getIssues()
	if err != nil && len(issues) == 0 {
		http.Error(w, "failed to fetch issues: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	if len(issues) == 0 {
		fmt.Fprint(w, `<p class="size-h5 color-subdue">No pending Renovate updates</p>`)
		return
	}

	var b strings.Builder
	b.WriteString(`<ul class="list list-gap-10 collapsible-container" data-collapse-after="5">`)
	for _, issue := range issues {
		fmt.Fprintf(&b,
			`<li><a class="size-h4 color-primary-if-not-visited" href="%s" target="_blank">%s</a>`+
				`<p class="size-h6 color-subdue">%s &bull; %s</p></li>`,
			html.EscapeString(issue.URL),
			html.EscapeString(issue.Title),
			html.EscapeString(issue.Repo),
			issue.Age,
		)
	}
	b.WriteString(`</ul>`)
	fmt.Fprint(w, b.String())
}

func main() {
	githubToken = os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		log.Fatal("GITHUB_TOKEN is required")
	}

	reposEnv := os.Getenv("REPOS")
	if reposEnv == "" {
		log.Fatal("REPOS is required (comma-separated org/repo list)")
	}
	for _, r := range strings.Split(reposEnv, ",") {
		if r = strings.TrimSpace(r); r != "" {
			repoList = append(repoList, r)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", handler)
	log.Printf("listening on :%s, watching: %v", port, repoList)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
