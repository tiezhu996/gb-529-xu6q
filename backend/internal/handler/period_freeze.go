package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"lng-boiloff-gas-balance/backend/internal/dto"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/internal/service"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

type FreezeHandler struct {
	service *service.FreezeService
}

func NewFreezeHandler(freezeService *service.FreezeService) *FreezeHandler {
	return &FreezeHandler{service: freezeService}
}

func (h *FreezeHandler) List(c *gin.Context) {
	page, pageSize := pagination(c)
	tankID, _ := strconv.ParseUint(c.Query("tank_id"), 10, 32)
	from, ok := optionalTimeQuery(c, "from")
	if !ok {
		return
	}
	to, ok := optionalTimeQuery(c, "to")
	if !ok {
		return
	}
	filter := repository.FreezeFilter{
		TankID: uint(tankID), Status: c.Query("status"),
		From: from, To: to, Page: page, PageSize: pageSize,
	}
	items, total, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, items, page, pageSize, total)
}

func (h *FreezeHandler) Get(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}

func (h *FreezeHandler) Create(c *gin.Context) {
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.CreateFreezeRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Create(c.Request.Context(), request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusCreated, item)
}

func (h *FreezeHandler) Review(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.ReviewFreezeRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Review(c.Request.Context(), id, request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}

func (h *FreezeHandler) Release(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.ReleaseFreezeRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Release(c.Request.Context(), id, request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}
