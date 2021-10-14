package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/highlight-run/highlight-go"
)

// Middleware is a gin compatible middleware
// use as follows:
//
// import highlightGin "github.com/highlight-run/highlight-go/pkg/gin/middleware"
// ...
// r.Use(highlightGin.Middleware())
//
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		highlightReqDetails := c.GetHeader("X-Highlight-Request")
		ids := strings.Split(highlightReqDetails, "/")
		if len(ids) < 2 {
			return
		}
		c.Set(highlight.ContextKeys.HighlightSessionID, ids[0])
		c.Set(highlight.ContextKeys.HighlightRequestID, ids[1])
	}
}
