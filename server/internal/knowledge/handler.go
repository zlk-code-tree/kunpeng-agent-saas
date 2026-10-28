package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"

	"gin-dify-saas/server/internal/platform/httpx"
	"gin-dify-saas/server/internal/platform/requestctx"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

const maxUploadSize = 50 << 20

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Upload(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)
	header, err := c.FormFile("file")
	if err != nil {
		httpx.Error(c, 400, 14210, "file is required and must not exceed 50 MB")
		return
	}
	file, err := header.Open()
	if err != nil {
		httpx.Error(c, 400, 14211, "failed to open upload")
		return
	}
	defer file.Close()
	checksum, err := checksumFile(file)
	if err != nil {
		httpx.Error(c, 500, 15210, "failed to checksum upload")
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	item, err := h.service.Upload(c.Request.Context(), identity.TenantID, identity.UserID, c.Param("id"), header.Filename, contentType, checksum, header.Size, file)
	if err != nil {
		httpx.Error(c, 500, 15211, "failed to upload document")
		return
	}
	httpx.Created(c, item)
}

func (h *Handler) ListDocuments(c *gin.Context) {
	identity, _ := requestctx.Get(c)
	items, err := h.service.ListDocuments(c.Request.Context(), identity.TenantID, identity.UserID, c.Param("id"))
	if err != nil {
		httpx.Error(c, 500, 15212, "failed to list documents")
		return
	}
	httpx.OK(c, items)
}

func checksumFile(file multipart.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (h *Handler) List(c *gin.Context) {
	identity, ok := requestctx.Get(c)
	if !ok {
		httpx.Error(c, 401, 14010, "missing identity")
		return
	}
	items, err := h.service.List(c.Request.Context(), identity.TenantID, identity.UserID)
	if err != nil {
		httpx.Error(c, 500, 15200, "failed to list knowledge bases")
		return
	}
	httpx.OK(c, items)
}

func (h *Handler) Create(c *gin.Context) {
	identity, ok := requestctx.Get(c)
	if !ok {
		httpx.Error(c, 401, 14010, "missing identity")
		return
	}
	var input CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httpx.Error(c, 400, 14201, "invalid knowledge base input")
		return
	}
	item, err := h.service.Create(c.Request.Context(), identity.TenantID, identity.UserID, input)
	if err != nil {
		httpx.Error(c, 500, 15201, "failed to create knowledge base")
		return
	}
	httpx.Created(c, item)
}
