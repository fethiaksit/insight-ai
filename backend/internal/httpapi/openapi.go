package httpapi

import _ "embed"

//go:embed docs/openapi.yaml
var openAPISpec []byte
