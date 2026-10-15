package httpapi

import (
	"bytes"
	"context"

	"github.com/saudm6/dfm-be/api"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

func (server) GetOpenAPISpec(context.Context, gen.GetOpenAPISpecRequestObject) (gen.GetOpenAPISpecResponseObject, error) {
	return gen.GetOpenAPISpec200ApplicationyamlResponse{
		Body:          bytes.NewReader(api.OpenAPI),
		ContentLength: int64(len(api.OpenAPI)),
	}, nil
}
