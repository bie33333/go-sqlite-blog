package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AndreHeber/go-sqlite-blog/models"
)

func TestWriteIsolatedHTMLScriptPermissionsAndNavigation(t *testing.T) {
	for _, allowScripts := range []bool{false, true} {
		t.Run(map[bool]string{false: "scripts disabled by default", true: "scripts enabled in sandbox"}[allowScripts], func(t *testing.T) {
			response := httptest.NewRecorder()
			page := models.Page{
				Title:        `<Page>`,
				Slug:         "demo",
				Content:      `<!doctype html><html><body><script>window.demo = true;</script><main>Content</main></body></html>`,
				AllowScripts: allowScripts,
			}
			tags := []models.Tag{{ID: 7, Name: "Solana"}}
			if err := writeIsolatedHTML(response, page, tags, nil); err != nil {
				t.Fatal(err)
			}

			policy := response.Header().Get("Content-Security-Policy")
			if !strings.Contains(policy, "sandbox") {
				t.Fatal("response CSP must isolate custom HTML")
			}
			if allowScripts {
				if !strings.Contains(policy, "sandbox allow-scripts") || strings.Contains(policy, "allow-same-origin") {
					t.Fatalf("unexpected script-enabled policy: %s", policy)
				}
			} else if !strings.Contains(policy, "script-src 'none'") || strings.Contains(policy, "allow-scripts") {
				t.Fatalf("scripts should remain disabled: %s", policy)
			}

			body := response.Body.String()
			if !strings.Contains(body, `href="/?tag=7"`) ||
				!strings.Contains(body, `&lt;Page&gt;`) ||
				!strings.Contains(body, `<main>Content</main>`) ||
				!strings.Contains(body, `<script>window.demo = true;</script>`) {
				t.Fatalf("navigation or original HTML missing/unsafe: %s", body)
			}
		})
	}
}
