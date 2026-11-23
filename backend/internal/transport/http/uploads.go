package http

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxImageSize = 5 << 20

func (catalog *Catalog) uploadImage(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImageSize+(1<<20))
	if err := c.Request.ParseMultipartForm(2 << 20); err != nil {
		badRequest(c, "Không thể đọc dữ liệu ảnh hoặc file vượt quá giới hạn")
		return
	}
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		badRequest(c, "Vui lòng chọn file ảnh ở trường image")
		return
	}
	defer file.Close()
	if header.Size > maxImageSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ảnh không được vượt quá 5 MB"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxImageSize+1))
	if err != nil {
		badRequest(c, "Không thể đọc file ảnh")
		return
	}
	if len(data) == 0 || len(data) > maxImageSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ảnh phải có dung lượng từ 1 byte đến 5 MB"})
		return
	}
	contentType := http.DetectContentType(data[:min(len(data), 512)])
	extensions := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif"}
	extension, ok := extensions[strings.Split(contentType, ";")[0]]
	if !ok {
		badRequest(c, "Chỉ chấp nhận ảnh JPEG, PNG hoặc GIF")
		return
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		badRequest(c, "File tải lên không phải ảnh hợp lệ")
		return
	}
	var randomName [16]byte
	if _, err := rand.Read(randomName[:]); err != nil {
		internalError(c)
		return
	}
	if err := os.MkdirAll(catalog.uploadDir, 0o755); err != nil {
		internalError(c)
		return
	}
	filename := hex.EncodeToString(randomName[:]) + extension
	destination := filepath.Join(catalog.uploadDir, filename)
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		internalError(c)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"url": fmt.Sprintf("/uploads/%s", filename), "content_type": contentType, "size": len(data)})
}
