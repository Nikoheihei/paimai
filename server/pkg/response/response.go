package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 定义了统一的 API 响应结构体。
type Response struct {
	Code    int         `json:"code"`           // 业务错误码或状态码
	Message string      `json:"message"`        // 描述信息
	Data    interface{} `json:"data,omitempty"` // 数据载荷
}

// Success 以 HTTP 200 返回统一的成功响应。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// Fail 以 HTTP 200 返回业务失败响应，通过 Code 标识具体业务错误。
func Fail(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusOK, Response{
		Code:    code,
		Message: msg,
	})
}

// Error 以指定 HTTP 状态码返回系统错误或接口级错误响应。
func Error(c *gin.Context, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, Response{
		Code:    code,
		Message: msg,
	})
}
