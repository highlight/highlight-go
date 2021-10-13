package middleware

import (
	"net/http"

	"github.com/highlight-run/highlight-go"
)

// Middleware is a go-chi compatible middleware
// use as follows:
//
// import highlight_chi "github.com/highlight-run/highlight-go/pkg/go-chi/middleware"
// ...
// r.Use(highlight_chi.Middleware)
//
func Middleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ctx := highlight.InterceptRequest(r)
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}
