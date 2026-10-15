package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/documents"
	"github.com/saudm6/dfm-be/internal/domain"
	"github.com/saudm6/dfm-be/internal/httpapi/gen"
)

// maxDocumentRequest caps the PUT /me/documents/{kind}/{name} request body,
// and with it the document body inside.
const maxDocumentRequest = documents.MaxBodyBytes

// documentGuard reads the body of PUT /me/documents/{kind}/{name} before the
// generated code decodes it, and answers one over maxDocumentRequest bytes
// with 413.
type documentGuard struct{ gen.ServerInterface }

func (g documentGuard) PutMyDocument(w http.ResponseWriter, r *http.Request, kind gen.DocumentKindParam, name gen.DocumentNameParam, params gen.PutMyDocumentParams) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDocumentRequest))
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		writeError(w, http.StatusRequestEntityTooLarge, "content_too_large", fmt.Sprintf("request body is larger than %d bytes", maxDocumentRequest))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "request body could not be read")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	g.ServerInterface.PutMyDocument(w, r, kind, name, params)
}

func (s server) ListMyDocuments(ctx context.Context, req gen.ListMyDocumentsRequestObject) (gen.ListMyDocumentsResponseObject, error) {
	sess, ok := auth.SessionFromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	infos, err := s.docs.List(ctx, sess.User.ID, req.Kind)
	if err != nil {
		return nil, err
	}
	list := gen.ListMyDocuments200JSONResponse{Documents: make([]gen.DocumentInfo, 0, len(infos))}
	for _, info := range infos {
		list.Documents = append(list.Documents, documentInfo(info))
	}
	return list, nil
}

func (s server) GetMyDocument(ctx context.Context, req gen.GetMyDocumentRequestObject) (gen.GetMyDocumentResponseObject, error) {
	sess, ok := auth.SessionFromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	name, err := documents.ParseName(req.Name)
	if err != nil {
		return nil, invalidParam("name", err)
	}
	doc, err := s.docs.Get(ctx, sess.User.ID, req.Kind, name)
	if errors.Is(err, documents.ErrNotFound) {
		return gen.GetMyDocument404JSONResponse(documentNotFound(req.Kind, name)), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetMyDocument200JSONResponse{
		Body:    gen.Document{Kind: req.Kind, Name: name, SchemaVersion: doc.SchemaVersion, Body: doc.Body},
		Headers: gen.GetMyDocument200ResponseHeaders{ETag: doc.ETag},
	}, nil
}

func (s server) PutMyDocument(ctx context.Context, req gen.PutMyDocumentRequestObject) (gen.PutMyDocumentResponseObject, error) {
	sess, ok := auth.SessionFromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	name, err := documents.ParseName(req.Name)
	if err != nil {
		return nil, invalidParam("name", err)
	}
	if req.Params.IfMatch == nil {
		info, err := s.docs.Create(ctx, sess.User.ID, req.Kind, name, req.Body.SchemaVersion, req.Body.Body)
		switch {
		case errors.Is(err, documents.ErrExists):
			return gen.PutMyDocument428JSONResponse{
				Code:    "precondition_required",
				Message: fmt.Sprintf("%s %q exists; send If-Match with its ETag to replace it", req.Kind, name),
			}, nil
		case errors.Is(err, domain.ErrInvalid):
			return gen.PutMyDocumentdefaultJSONResponse{StatusCode: http.StatusBadRequest, Body: badRequest(err)}, nil
		case err != nil:
			return nil, err
		}
		return gen.PutMyDocument201JSONResponse{Body: documentInfo(info), Headers: gen.PutMyDocument201ResponseHeaders{ETag: info.ETag}}, nil
	}
	cond, err := documents.ParseIfMatch(*req.Params.IfMatch)
	if err != nil {
		return gen.PutMyDocumentdefaultJSONResponse{StatusCode: http.StatusBadRequest, Body: badRequest(err)}, nil
	}
	info, err := s.docs.Update(ctx, sess.User.ID, req.Kind, name, cond, req.Body.SchemaVersion, req.Body.Body)
	switch {
	case errors.Is(err, documents.ErrPreconditionFailed):
		return gen.PutMyDocument412JSONResponse(documentChanged(req.Kind, name)), nil
	case errors.Is(err, domain.ErrInvalid):
		return gen.PutMyDocumentdefaultJSONResponse{StatusCode: http.StatusBadRequest, Body: badRequest(err)}, nil
	case err != nil:
		return nil, err
	}
	return gen.PutMyDocument200JSONResponse{Body: documentInfo(info), Headers: gen.PutMyDocument200ResponseHeaders{ETag: info.ETag}}, nil
}

func (s server) DeleteMyDocument(ctx context.Context, req gen.DeleteMyDocumentRequestObject) (gen.DeleteMyDocumentResponseObject, error) {
	sess, ok := auth.SessionFromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	name, err := documents.ParseName(req.Name)
	if err != nil {
		return nil, invalidParam("name", err)
	}
	if req.Params.IfMatch == nil {
		return gen.DeleteMyDocument428JSONResponse{
			Code:    "precondition_required",
			Message: "send If-Match with the document's ETag to delete it",
		}, nil
	}
	cond, err := documents.ParseIfMatch(*req.Params.IfMatch)
	if err != nil {
		return gen.DeleteMyDocumentdefaultJSONResponse{StatusCode: http.StatusBadRequest, Body: badRequest(err)}, nil
	}
	err = s.docs.Delete(ctx, sess.User.ID, req.Kind, name, cond)
	switch {
	case errors.Is(err, documents.ErrNotFound):
		return gen.DeleteMyDocument404JSONResponse(documentNotFound(req.Kind, name)), nil
	case errors.Is(err, documents.ErrPreconditionFailed):
		return gen.DeleteMyDocument412JSONResponse(documentChanged(req.Kind, name)), nil
	case err != nil:
		return nil, err
	}
	return gen.DeleteMyDocument204Response{}, nil
}

func documentInfo(info documents.Info) gen.DocumentInfo {
	return gen.DocumentInfo{Name: info.Name, ETag: info.ETag, UpdatedAt: info.UpdatedAt}
}

func documentNotFound(kind domain.DocumentKind, name string) gen.Error {
	return gen.Error{Code: "not_found", Message: fmt.Sprintf("no %s named %q", kind, name)}
}

func documentChanged(kind domain.DocumentKind, name string) gen.Error {
	return gen.Error{Code: "precondition_failed", Message: fmt.Sprintf("%s %q has changed or does not exist; get it again", kind, name)}
}

func badRequest(err error) gen.Error {
	return gen.Error{Code: "bad_request", Message: detail(err)}
}
