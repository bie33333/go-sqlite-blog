package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndreHeber/go-sqlite-blog/middleware"
	"github.com/AndreHeber/go-sqlite-blog/models"
)

const (
	// UploadsDir 是图片上传目录（相对于项目根目录），由 main.go 挂载到 /uploads/。
	UploadsDir = "static/uploads"
	// UploadsURLPrefix 是图片对外访问的前缀。
	UploadsURLPrefix = "/uploads/"
	// UploadsPostPath 是上传接口的路径。
	// 单独用不带尾斜杠的路径，避免 ServeMux 把 POST /uploads 307 重定向到 /uploads/。
	UploadsPostPath = "/uploads"

	maxUploadBytes = 5 << 20 // 5 MB
)

// allowedImageTypes 把探测到的真实 MIME 映射到落盘扩展名。
// 不信任客户端提供的文件名与 Content-Type，只信 http.DetectContentType 的结果。
var allowedImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// UploadImage 处理文章图片上传（需登录）。
//
// 请求：multipart/form-data，文件字段名 image。
// 响应：Accept 含 application/json 时返回 {"url":..., "markdown":...}，
// 否则 303 跳回编辑器（无 JS 时的降级路径）。
func UploadImage(a *middleware.Adapter) error {
	r := a.Request
	w := a.ResponseWriter

	session, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}

	// 限制请求体大小，避免把超大文件读进内存
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return uploadError(a, fmt.Errorf("UploadImage: %w", err), "图片过大或格式不正确（上限 5 MB）")
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		return uploadError(a, fmt.Errorf("UploadImage: %w", err), "请选择要上传的图片")
	}
	defer file.Close()

	if header.Size > maxUploadBytes {
		return uploadError(a, errors.New("UploadImage: file too large"), "图片过大（上限 5 MB）")
	}

	// 读取文件头判断真实类型，防止把任意文件改名成 .png 上传
	head := make([]byte, 512)
	n, err := file.Read(head)
	if err != nil && n == 0 {
		return uploadError(a, fmt.Errorf("UploadImage: %w", err), "无法读取上传的文件")
	}

	ext, ok := allowedImageTypes[detectImageType(head[:n])]
	if !ok {
		return uploadError(a, errors.New("UploadImage: unsupported image type"),
			"只支持 PNG / JPEG / GIF / WebP 图片")
	}

	if _, err := file.Seek(0, 0); err != nil {
		return uploadError(a, fmt.Errorf("UploadImage: %w", err), "无法读取上传的文件")
	}

	if err := os.MkdirAll(UploadsDir, 0o755); err != nil {
		return fmt.Errorf("UploadImage: %w", err)
	}

	// 文件名完全由服务端生成，不使用用户提供的名字，避免路径穿越与相互覆盖
	randomName, err := randomHex(16)
	if err != nil {
		return fmt.Errorf("UploadImage: %w", err)
	}
	storedName := randomName + ext
	storedPath := filepath.Join(UploadsDir, storedName)

	dst, err := os.OpenFile(storedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("UploadImage: %w", err)
	}

	if _, err := dst.ReadFrom(file); err != nil {
		dst.Close()
		os.Remove(storedPath)
		return fmt.Errorf("UploadImage: %w", err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(storedPath)
		return fmt.Errorf("UploadImage: %w", err)
	}

	url := UploadsURLPrefix + storedName
	originalName := sanitizeDisplayName(header.Filename)

	// 记录到 media 表（表已存在，之前没有任何地方使用）
	env := models.EnvFromAdapter(a)
	if _, err := env.DB.ExecContext(env.Ctx,
		`INSERT INTO media (file_name, file_path, uploaded_by) VALUES (?, ?, ?)`,
		originalName, url, session.UserID); err != nil {
		// 记录失败不影响图片本身可用，只记日志
		a.Logger.Error("UploadImage: failed to record media row", "error", err, "path", url)
	}

	markdown := fmt.Sprintf("![%s](%s)", originalName, url)

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		return json.NewEncoder(w).Encode(map[string]string{
			"url":      url,
			"filename": originalName,
			"markdown": markdown,
		})
	}

	http.Redirect(w, r, "/post/new", http.StatusSeeOther)
	return nil
}

// uploadError 统一处理上传失败：JSON 请求返回 4xx + 错误信息，表单请求重渲染编辑器。
func uploadError(a *middleware.Adapter, cause error, message string) error {
	a.Logger.Error("handlers: upload failed", "error", cause)

	if wantsJSON(a.Request) {
		a.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
		a.ResponseWriter.WriteHeader(http.StatusBadRequest)
		return json.NewEncoder(a.ResponseWriter).Encode(map[string]string{"error": message})
	}

	return renderPage(a, "editor.html", ViewData{
		Title: "写文章",
		Error: message,
	})
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// detectImageType 返回 http.DetectContentType 的结果（去掉 charset 等参数）。
func detectImageType(head []byte) string {
	contentType := http.DetectContentType(head)
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.TrimSpace(contentType)
}

// sanitizeDisplayName 只保留文件名部分并限制长度，仅用于展示/alt 文本。
func sanitizeDisplayName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || name == "/" {
		return "image"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	// alt 文本里出现 ] 会破坏 markdown 语法
	return strings.ReplaceAll(name, "]", "")
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
