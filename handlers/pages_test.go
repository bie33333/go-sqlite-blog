package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AndreHeber/go-sqlite-blog/models"
)

func TestWriteHTMLScriptPermissionsWithoutNavigation(t *testing.T) {
	for _, allowScripts := range []bool{false, true} {
		t.Run(map[bool]string{false: "scripts disabled by default", true: "scripts enabled with same-origin access"}[allowScripts], func(t *testing.T) {
			response := httptest.NewRecorder()
			page := models.Page{
				Title:        `<Page>`,
				Slug:         "demo",
				Content:      `<!doctype html><html><body><script>window.demo = true;</script><main>Content</main></body></html>`,
				AllowScripts: allowScripts,
			}
			if err := writeIsolatedHTML(response, page); err != nil {
				t.Fatal(err)
			}

			policy := response.Header().Get("Content-Security-Policy")
			if allowScripts {
				if strings.Contains(policy, "sandbox") ||
					!strings.Contains(policy, "script-src 'unsafe-inline' https:") ||
					!strings.Contains(policy, "connect-src https: wss:") {
					t.Fatalf("script-enabled page should have same-origin access and websocket connectivity: %s", policy)
				}
			} else if !strings.Contains(policy, "script-src 'none'; sandbox") || strings.Contains(policy, "allow-scripts") {
				t.Fatalf("scripts should remain disabled: %s", policy)
			}

			body := response.Body.String()
			if strings.Contains(body, "blog-site-navigation") ||
				strings.Contains(body, `href="/?tag=7"`) ||
				!strings.Contains(body, `<main>Content</main>`) ||
				!strings.Contains(body, `<script>window.demo = true;</script>`) {
				t.Fatalf("unexpected navigation or missing original HTML: %s", body)
			}
		})
	}
}
