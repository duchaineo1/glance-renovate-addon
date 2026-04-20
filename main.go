package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

// matches unchecked Renovate dashboard checkboxes with a key=value comment (update items);
// control items like <!-- rebase-all-open-prs --> have no = and are excluded by this pattern
var itemRe = regexp.MustCompile(`(?m)^\s*- \[ \] <!-- *[\w-]+=\S[^>]*-->\s*(.+)$`)

type ghIssue struct {
	Title       string    `json:"title"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	PullRequest *struct{} `json:"pull_request"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

type repoResult struct {
	Name  string
	URL   string
	Items []string
}

var (
	githubToken string
	repoList    []string
	mu          sync.Mutex
	cached      []repoResult
	cacheTime   time.Time
)

var (
	dockerUpdateRe  = regexp.MustCompile(`(?i)^Update (.+?) Docker tag to (.+)$`)
	generalUpdateRe = regexp.MustCompile(`(?i)^Update (.+?) to (.+)$`)
)

func cleanItem(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, " -> ", " → ")
	return strings.TrimSpace(s)
}

func formatItem(s string) string {
	if m := dockerUpdateRe.FindStringSubmatch(s); m != nil {
		image, version := m[1], m[2]
		if strings.Contains(version, " → ") {
			return image + ": " + version
		}
		return image + " → " + version
	}
	if m := generalUpdateRe.FindStringSubmatch(s); m != nil {
		pkg, version := m[1], m[2]
		if strings.Contains(version, " → ") {
			return pkg + ": " + version
		}
		return pkg + " → " + version
	}
	return s
}

func parseItems(body string) []string {
	matches := itemRe.FindAllStringSubmatch(body, -1)
	items := make([]string, 0, len(matches))
	for _, m := range matches {
		if item := formatItem(cleanItem(m[1])); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func fetchIssues() ([]repoResult, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var results []repoResult

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
				continue
			}
			items := parseItems(issue.Body)
			if len(items) > 0 {
				results = append(results, repoResult{
					Name:  shortName,
					URL:   issue.HTMLURL,
					Items: items,
				})
			}
		}
	}
	return results, nil
}

func getIssues() ([]repoResult, error) {
	mu.Lock()
	defer mu.Unlock()
	if time.Since(cacheTime) < cacheTTL {
		return cached, nil
	}
	results, err := fetchIssues()
	if err != nil {
		return cached, err // return stale on error
	}
	cached = results
	cacheTime = time.Now()
	return cached, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	results, err := getIssues()
	if err != nil && len(results) == 0 {
		http.Error(w, "failed to fetch issues: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Widget-Content-Type", "html")

	if len(results) == 0 {
		fmt.Fprint(w, `<p class="size-h5 color-subdue">No pending Renovate updates</p>`)
		return
	}

	var b strings.Builder
	b.WriteString(`<ul class="list list-gap-10 collapsible-container" data-collapse-after="5">`)
	for _, repo := range results {
		fmt.Fprintf(&b,
			`<li><a class="size-h4 block text-truncate color-primary-if-not-visited" href="%s" target="_blank" rel="noreferrer">%s</a>`,
			html.EscapeString(repo.URL),
			html.EscapeString(repo.Name),
		)
		b.WriteString(`<ul class="list list-gap-4 margin-top-4">`)
		for _, item := range repo.Items {
			fmt.Fprintf(&b, `<li class="size-h5 color-subdue text-truncate">%s</li>`, html.EscapeString(item))
		}
		b.WriteString(`</ul></li>`)
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
