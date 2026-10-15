package httpapi

import (
	"context"

	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

func (server) GetHealth(context.Context, gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	return gen.GetHealth200JSONResponse{Status: gen.HealthStatusOK}, nil
}
