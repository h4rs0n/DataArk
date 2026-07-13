package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
)

type fixtureUser struct {
	ID           uint     `json:"id"`
	Timezone     string   `json:"timezone"`
	Feedback     string   `json:"feedback"`
	BlockedSites []string `json:"blocked_sites"`
}

type deterministicSiteWorld struct {
	A       *httptest.Server
	B       *httptest.Server
	C       *httptest.Server
	Fetcher HTTPFetcher
	files   fs.FS
}

func newDeterministicSiteWorld(t *testing.T) *deterministicSiteWorld {
	t.Helper()
	world := &deterministicSiteWorld{files: osDirFS("testdata/sites")}
	world.A = httptest.NewUnstartedServer(world.siteHandler("a"))
	world.B = httptest.NewUnstartedServer(world.siteHandler("b"))
	world.C = httptest.NewUnstartedServer(world.siteHandler("c"))
	world.A.Start()
	world.B.Start()
	world.C.Start()
	world.Fetcher = HTTPClientFetcher{Client: world.A.Client(), Clock: SystemClock{}}
	t.Cleanup(func() {
		world.A.Close()
		world.B.Close()
		world.C.Close()
	})
	return world
}

// osDirFS is a variable so tests that need a corrupt or missing fixture tree can
// replace it without changing the process working directory.
var osDirFS = func(root string) fs.FS { return os.DirFS(root) }

func (world *deterministicSiteWorld) siteHandler(site string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if world.serveBehaviorEndpoint(writer, request, site) {
			return
		}
		fixturePath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if fixturePath == "." || fixturePath == "" {
			fixturePath = "home.html"
		}
		fixturePath = site + "/" + fixturePath
		body, err := fs.ReadFile(world.files, fixturePath)
		if err != nil && site == "b" && strings.HasPrefix(request.URL.Path, "/articles/normal-") {
			body = []byte("<!doctype html><html lang=\"en\"><head><title>Routine note</title></head><body><article><h1>Routine note</h1><p>A routine update.</p></article></body></html>")
			err = nil
		}
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		body = world.expand(body)
		switch path.Ext(fixturePath) {
		case ".xml":
			writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
		case ".json":
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		case ".txt":
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		default:
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		_, _ = writer.Write(body)
	})
}

func (world *deterministicSiteWorld) serveBehaviorEndpoint(writer http.ResponseWriter, request *http.Request, site string) bool {
	if site != "a" {
		return false
	}
	switch request.URL.Path {
	case "/conditional.xml":
		writer.Header().Set("Content-Type", "application/rss+xml")
		writer.Header().Set("ETag", `"site-a-v1"`)
		writer.Header().Set("Last-Modified", "Sun, 12 Jul 2026 08:00:00 GMT")
		if request.Header.Get("If-None-Match") == `"site-a-v1"` || request.Header.Get("If-Modified-Since") == "Sun, 12 Jul 2026 08:00:00 GMT" {
			writer.WriteHeader(http.StatusNotModified)
			return true
		}
		body, _ := fs.ReadFile(world.files, "a/feed.xml")
		_, _ = writer.Write(world.expand(body))
		return true
	case "/fail":
		http.Error(writer, "fixture failure", http.StatusInternalServerError)
		return true
	case "/timeout":
		<-request.Context().Done()
		return true
	case "/redirect/same":
		http.Redirect(writer, request, world.A.URL+"/articles/shared.html", http.StatusFound)
		return true
	case "/redirect/cross":
		http.Redirect(writer, request, world.C.URL+"/", http.StatusFound)
		return true
	default:
		return false
	}
}

func (world *deterministicSiteWorld) expand(body []byte) []byte {
	replacer := strings.NewReplacer("{{A}}", world.A.URL, "{{B}}", world.B.URL, "{{C}}", world.C.URL)
	return []byte(replacer.Replace(string(body)))
}

func (world *deterministicSiteWorld) users(t *testing.T) []fixtureUser {
	t.Helper()
	body, err := fs.ReadFile(world.files, "users.json")
	if err != nil {
		t.Fatal(err)
	}
	var users []fixtureUser
	if err := json.Unmarshal(world.expand(body), &users); err != nil {
		t.Fatal(err)
	}
	return users
}

func TestDeterministicSiteWorldCoversDiscoveryScenarios(t *testing.T) {
	world := newDeterministicSiteWorld(t)
	ctx := context.Background()

	fetch := func(rawURL string, etag string, lastModified string) FetchResult {
		t.Helper()
		response, err := world.Fetcher.Fetch(ctx, FetchRequest{URL: rawURL, ETag: etag, LastModified: lastModified, MaxBytes: 128 << 10})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	aLinks := fetch(world.A.URL+"/links.html", "", "")
	if !strings.Contains(string(aLinks.Body), world.B.URL) || !strings.Contains(string(aLinks.Body), world.C.URL) {
		t.Fatalf("seed A blogroll does not reference B and C: %s", aLinks.Body)
	}
	bBlogroll := fetch(world.B.URL+"/blogroll.html", "", "")
	if !strings.Contains(string(bBlogroll.Body), world.A.URL) || !strings.Contains(string(bBlogroll.Body), world.C.URL) {
		t.Fatalf("B blogroll does not close the graph cycle: %s", bBlogroll.Body)
	}
	cHome := fetch(world.C.URL+"/", "", "")
	for _, marker := range []string{"news.example", "shop.example", "social.example", world.A.URL + "/articles/shared.html"} {
		if !strings.Contains(string(cHome.Body), marker) {
			t.Fatalf("C fixture missing %q", marker)
		}
	}

	bFeed := fetch(world.B.URL+"/feed.xml", "", "")
	candidates, err := parseFeedCandidates(bFeed.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 13 {
		t.Fatalf("B feed candidate count = %d, want twelve routine plus one high-value article", len(candidates))
	}
	sitemap := fetch(world.B.URL+"/sitemap.xml", "", "")
	archive := fetch(world.B.URL+"/archive/page-2.html", "", "")
	if !strings.Contains(string(sitemap.Body), "high-sitemap.html") || strings.Contains(string(bFeed.Body), "high-sitemap.html") {
		t.Fatal("sitemap-only high-value article is not isolated to the old sitemap")
	}
	if !strings.Contains(string(archive.Body), "high-archive.html") || strings.Contains(string(bFeed.Body), "high-archive.html") {
		t.Fatal("archive-only high-value article is not isolated to archive pagination")
	}
	if !strings.Contains(string(sitemap.Body), world.A.URL+"/articles/shared.html") || !strings.Contains(string(cHome.Body), world.A.URL+"/articles/shared.html") {
		t.Fatal("shared article is not present in feed, sitemap, and cross-site link fixtures")
	}

	robots := fetch(world.A.URL+"/robots.txt", "", "")
	if !strings.Contains(string(robots.Body), "Disallow: /private/") {
		t.Fatalf("robots fixture = %s", robots.Body)
	}
	if fetch(world.A.URL+"/fail", "", "").StatusCode != http.StatusInternalServerError {
		t.Fatal("failure fixture did not return 500")
	}
	redirect := fetch(world.A.URL+"/redirect/cross", "", "")
	if redirect.FinalURL != world.C.URL+"/" {
		t.Fatalf("cross-site redirect final URL = %q", redirect.FinalURL)
	}

	first := fetch(world.A.URL+"/conditional.xml", "", "")
	second := fetch(world.A.URL+"/conditional.xml", first.ETag, "")
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusNotModified || len(second.Body) != 0 {
		t.Fatalf("conditional fixture responses = %d then %d (%d bytes)", first.StatusCode, second.StatusCode, len(second.Body))
	}

	users := world.users(t)
	if len(users) != 2 || users[0].Timezone == users[1].Timezone || len(users[0].BlockedSites) != 0 || fmt.Sprint(users[1].BlockedSites) != fmt.Sprint([]string{world.B.URL}) {
		t.Fatalf("user fixtures = %#v", users)
	}
}
