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

type ghPR struct {
	Title     string    `json:"title"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	User      struct {
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

func fetchPRs() ([]prEntry, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var entries []prEntry

	for _, repo := range repoList {
		url := fmt.Sprintf("https://api.github.com/repos/%s/pulls?state=open&per_page=100", repo)
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

		var prs []ghPR
		if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
			return nil, err
		}

		shortName := repo
		if i := strings.Index(repo, "/"); i >= 0 {
			shortName = repo[i+1:]
		}

		for _, pr := range prs {
			if pr.User.Login == "renovate[bot]" {
				entries = append(entries, prEntry{
					Title: pr.Title,
					URL:   pr.HTMLURL,
					Repo:  shortName,
					Age:   fmtAge(pr.CreatedAt),
				})
			}
		}
	}
	return entries, nil
}

func getPRs() ([]prEntry, error) {
	mu.Lock()
	defer mu.Unlock()
	if time.Since(cacheTime) < cacheTTL {
		return cached, nil
	}
	prs, err := fetchPRs()
	if err != nil {
		return cached, err // return stale on error
	}
	cached = prs
	cacheTime = time.Now()
	return cached, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	prs, err := getPRs()
	if err != nil && len(prs) == 0 {
		http.Error(w, "failed to fetch PRs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")

	if len(prs) == 0 {
		fmt.Fprint(w, `<p class="size-h5 color-subdue">No pending Renovate PRs</p>`)
		return
	}

	var b strings.Builder
	b.WriteString(`<ul class="list list-gap-10 collapsible-container" data-collapse-after="5">`)
	for _, pr := range prs {
		fmt.Fprintf(&b,
			`<li><a class="size-h4 color-primary-if-not-visited" href="%s" target="_blank">%s</a>`+
				`<p class="size-h6 color-subdue">%s &bull; %s</p></li>`,
			html.EscapeString(pr.URL),
			html.EscapeString(pr.Title),
			html.EscapeString(pr.Repo),
			pr.Age,
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
