package apidocs

import (
	_ "embed"
	"net/http"
)

// openapiSpec e a especificacao OpenAPI embutida no binario.
//
//go:embed openapi.yaml
var openapiSpec []byte

// swaggerUIPage carrega o Swagger UI via CDN e aponta para /openapi.yaml.
const swaggerUIPage = `<!doctype html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8">
  <title>Events API - Swagger UI</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/openapi.yaml", dom_id: "#swagger-ui" });
  </script>
</body>
</html>`

// OpenAPISpec serve o arquivo openapi.yaml.
func OpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(openapiSpec)
}

// SwaggerUI serve a pagina interativa de documentacao.
func SwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIPage))
}
