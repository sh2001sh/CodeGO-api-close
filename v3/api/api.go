package api

import (
	_ "embed"
	"net/http"
)

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0 -generate types -package api -o types.gen.go openapi.json
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0 -generate std-http-server -package api -o server.gen.go openapi.json
//go:generate go run ./generate

//go:embed openapi.json
var specification []byte

func Specification(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(specification)
}

type domainAdapter struct{ next http.Handler }

// DomainHandler binds and validates OpenAPI path/query types before dispatching
// to the independently authorized domain handlers. Unknown paths retain the
// composed handler's 404 or application-router behavior.
func DomainHandler(next http.Handler, onError func(http.ResponseWriter, *http.Request, error)) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", next)
	return HandlerWithOptions(&domainAdapter{next: next}, StdHTTPServerOptions{
		BaseRouter: mux, ErrorHandlerFunc: onError,
	})
}
