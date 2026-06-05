package handler

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"paimai/pkg/middleware"
	"paimai/pkg/response"
)

// uploadHandler 处理图片上传（Phase 1: 本地文件存储）
//
// POST /api/upload
// Content-Type: multipart/form-data
// Response: { code: 0, data: { url: "/uploads/xxx.jpg" } }
func RegisterUploadRoutes(r gin.IRouter) {
	upload := r.Group("/api", middleware.AuthRequired())
	{
		upload.POST("/upload", handleUpload)
	}
}

func handleUpload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 400, "缺少 file 字段")
		return
	}

	// 限制 5MB
	if file.Size > 5*1024*1024 {
		response.Error(c, http.StatusBadRequest, 400, "图片不能超过 5MB")
		return
	}

	// 检查 MIME 类型
	contentType := file.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" &&
		contentType != "image/gif" && contentType != "image/webp" {
		response.Error(c, http.StatusBadRequest, 400, "只支持 jpg/png/gif/webp 格式")
		return
	}

	// 保存到 uploads 目录（相对运行目录）
	ext := filepath.Ext(file.Filename)
	filename := generateUploadName() + ext

	// 使用 c.SaveUploadedFile 写入磁盘
	dest := filepath.Join("uploads", filename)
	if err := c.SaveUploadedFile(file, dest); err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "保存失败: "+err.Error())
		return
	}

	url := "/uploads/" + filename
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"url": url}})
}

// generateUploadName 生成唯一的上传文件名（时间戳 + 随机数）
func generateUploadName() string {
	// 简单实现：实际生产环境应使用 UUID 或更安全的方式
	return "img_" + randomString(12)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b[i] = letters[idx.Int64()]
	}
	return string(b)
}
