package v1

import (
	"net/http"

	"magitrickle/api/utils"
	"magitrickle/subscriptions"
)

type subscriptionSettings struct {
	FetchInterface string `json:"fetchInterface"`
}

func (h *Handler) GetSubscriptionSettings(w http.ResponseWriter, r *http.Request) {
	utils.WriteJson(w, http.StatusOK, subscriptionSettings{FetchInterface: h.app.Config().SubscriptionFetchInterface})
}

func (h *Handler) PutSubscriptionSettings(w http.ResponseWriter, r *http.Request) {
	req, err := utils.ReadJson[struct {
		FetchInterface *string `json:"fetchInterface"`
	}](r)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.FetchInterface == nil {
		utils.WriteError(w, http.StatusBadRequest, "fetchInterface is required")
		return
	}
	if err := subscriptions.ValidateFetchInterface(*req.FetchInterface); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.app.SetSubscriptionFetchInterface(*req.FetchInterface); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJson(w, http.StatusOK, subscriptionSettings{FetchInterface: *req.FetchInterface})
}
