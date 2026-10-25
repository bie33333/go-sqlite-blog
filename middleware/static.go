package middleware

import (
	"net/http"
	"strings"
)

// NoDirListing 包装 http.FileServer，禁止目录列表。
// 上传目录里都是用户上传的文件，暴露文件列表没有意义且有信息泄露风险。
func NoDirListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// StaticFiles 返回服务某个目录的 handler，并附带常见的安全与缓存响应头。
func StaticFiles(urlPrefix, dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))

	return NoDirListing(http.StripPrefix(urlPrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 上传内容类型由扩展名推断，禁止浏览器再自行嗅探
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// 图片不会变（文件名随机），可以长缓存
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fileServer.ServeHTTP(w, r)
	})))
}
