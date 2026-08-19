package server

import (
	"bytes"
	"context"

	"github.com/jaredeh/vmtool/internal/api"
	"github.com/jaredeh/vmtool/spec"
	"gopkg.in/yaml.v3"
)

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>vmtool API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css"/>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "/openapi.yaml",
      dom_id: "#swagger-ui",
      deepLinking: true
    });
  </script>
</body>
</html>
`

func specYAML() []byte { return spec.OpenAPIYAML }

func (h *handlers) GetDocs(_ context.Context, _ api.GetDocsRequestObject) (api.GetDocsResponseObject, error) {
	b := []byte(swaggerHTML)
	return api.GetDocs200TexthtmlResponse{
		Body:          bytes.NewReader(b),
		ContentLength: int64(len(b)),
	}, nil
}

func (h *handlers) GetOpenAPISpec(_ context.Context, _ api.GetOpenAPISpecRequestObject) (api.GetOpenAPISpecResponseObject, error) {
	y := specYAML()
	return api.GetOpenAPISpec200ApplicationyamlResponse{
		Body:          bytes.NewReader(y),
		ContentLength: int64(len(y)),
	}, nil
}

func (h *handlers) GetOpenAPIJSON(_ context.Context, _ api.GetOpenAPIJSONRequestObject) (api.GetOpenAPIJSONResponseObject, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(specYAML(), &doc); err != nil {
		return nil, err
	}
	return api.GetOpenAPIJSON200JSONResponse(doc), nil
}

type ctxKey int

const requestKey ctxKey = 1
