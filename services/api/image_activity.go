package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *apiServer) handleUserImagePublishActivity(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok || p.UserID() == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		ImageRef    string `json:"image_ref"`
		SourceImage string `json:"source_image,omitempty"`
		Mode        string `json:"mode,omitempty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	imageRef := strings.TrimSpace(req.ImageRef)
	if imageRef == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image_ref is required"})
		return
	}
	if len(imageRef) > 512 || len(req.SourceImage) > 512 || len(req.Mode) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image publish fields are too long"})
		return
	}
	message := strings.TrimSpace(req.Mode)
	if message != "" {
		message = "mode=" + message
	}
	s.platform.WriteAudit(r.Context(), auditEvent{
		UserID:       p.UserID(),
		Action:       "image_publish",
		Resource:     strings.TrimSpace(req.SourceImage),
		Namespace:    p.Namespace,
		Status:       "success",
		Message:      message,
		ActorIP:      requestIP(r),
		Source:       auditSource(r, p),
		AuthIdentity: auditIdentityLabel(p),
		ImageRef:     imageRef,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "image_ref": imageRef})
}
