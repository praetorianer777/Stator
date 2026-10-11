package httpapi

import (
	"net/http"

	"github.com/praetorianer777/stator/backend/internal/review"
)

func (s *Server) handleListReviewRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Pages.ReviewRules(r.Context(), actorFrom(r), spaceKey(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleSetSpaceReviewRule(w http.ResponseWriter, r *http.Request) {
	var req review.RuleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rule, lsn, err := s.Pages.SetSpaceReviewRule(r.Context(), actorFrom(r), spaceKey(r), req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleRemoveSpaceReviewRule(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Pages.RemoveSpaceReviewRule(r.Context(), actorFrom(r), spaceKey(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleSetPageReviewRule(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req review.RuleInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	rule, lsn, err := s.Pages.SetPageReviewRule(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) handleRemovePageReviewRule(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Pages.RemovePageReviewRule(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

func (s *Server) handleListReviews(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviews, err := s.Pages.Reviews(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"reviews": reviews})
}

func (s *Server) handleSubmitReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req review.SubmitInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	submitted, lsn, err := s.Pages.SubmitReview(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"review": submitted})
}

func (s *Server) handleGetReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviewID, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Pages.GetReview(r.Context(), actorFrom(r), id, reviewID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": found})
}

func (s *Server) handleDecideReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviewID, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req review.DecisionInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	decided, version, lsn, err := s.Pages.DecideReview(r.Context(), actorFrom(r), id, reviewID, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": decided, "version": version})
}

func (s *Server) handleWithdrawReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviewID, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	withdrawn, lsn, err := s.Pages.WithdrawReview(r.Context(), actorFrom(r), id, reviewID)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": withdrawn})
}

func (s *Server) handleSkipReview(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	reviewID, apiErr := pathUUID(r, "reviewID", "review")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req review.SkipInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	skipped, version, lsn, err := s.Pages.SkipReview(r.Context(), actorFrom(r), id, reviewID, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"review": skipped, "version": version})
}

func (s *Server) handleSetReadAsk(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req review.ReadAskInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	ask, lsn, err := s.Pages.SetReadAsk(r.Context(), actorFrom(r), id, req)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"readAsk": ask})
}

func (s *Server) handleConfirmRead(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	ask, lsn, err := s.Pages.ConfirmRead(r.Context(), actorFrom(r), id)
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"readAsk": ask})
}

func (s *Server) handleListReadConfirmations(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	readers, err := s.Pages.ReadConfirmations(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"readers": readers})
}
