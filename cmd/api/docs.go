package main

import (
	"encoding/json"
	"net/http"

	"github.com/Rahmannugar/livdot/internal/openapi"
	"github.com/gin-gonic/gin"
)

// scalarHTML renders the Scalar UI pointed at the served OpenAPI document.
// Scalar is only the documentation viewer; the document is the source of truth.
const scalarHTML = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>LIV DOT API</title>
  </head>
  <body>
    <script id="api-reference" data-url="/api/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`

// registerDocs mounts the OpenAPI document and the Scalar viewer under /api.
func registerDocs(router *gin.Engine) {
	document := openapi.Document()

	router.GET("/api/openapi.json", func(ctx *gin.Context) {
		encoded, err := json.Marshal(document)
		if err != nil {
			ctx.Status(http.StatusInternalServerError)
			return
		}
		ctx.Data(http.StatusOK, "application/json; charset=utf-8", encoded)
	})
	router.GET("/api/docs", func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(scalarHTML))
	})
}
