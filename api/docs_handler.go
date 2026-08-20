package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterDocs registers docs endpoints: /openapi.yaml and /docs (Swagger UI)
func RegisterDocs(r *gin.Engine) {
	r.GET("/openapi.yaml", func(c *gin.Context) {
		c.File("docs/openapi.yaml")
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!doctype html>
<html>
  <head>
    <meta charset="utf-8"/>
    <title>API Docs</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@4/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@4/swagger-ui-bundle.js"></script>
    <script>
      window.onload = function() {
        SwaggerUIBundle({
          url: '/openapi.yaml',
          dom_id: '#swagger-ui',
        });
      };
    </script>
  </body>
</html>`) 
	})
}
