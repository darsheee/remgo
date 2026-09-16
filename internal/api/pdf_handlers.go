package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// handleUploadPDF handles multipart or binary PDF uploads.
func (s *Server) handleUploadPDF(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)

	var filename string
	var reader io.Reader

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(100 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
			return
		}
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing 'file' field in multipart form")
			return
		}
		defer file.Close()
		filename = header.Filename
		reader = file
	} else {
		filename = r.URL.Query().Get("filename")
		if filename == "" {
			filename = r.Header.Get("X-Filename")
		}
		if filename == "" {
			filename = "document.pdf"
		}
		reader = r.Body
	}

	doc, err := s.db.SavePDF(userID, filename, reader)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// handleListPDFs lists all PDF documents belonging to the user.
func (s *Server) handleListPDFs(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	pdfs, err := s.db.ListPDFs(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pdfs)
}

// handleGetPDF returns metadata for a single PDF.
func (s *Server) handleGetPDF(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	doc, err := s.db.GetPDF(userID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, "pdf document not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleUpdatePDF updates mutable PDF document metadata like page count.
func (s *Server) handleUpdatePDF(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")

	var body struct {
		PageCount    *int    `json:"page_count"`
		OriginalName *string `json:"original_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	doc, err := s.db.GetPDF(userID, id)
	if err != nil || doc == nil {
		writeError(w, http.StatusNotFound, "pdf document not found")
		return
	}

	if body.PageCount != nil && *body.PageCount > 0 {
		if err := s.db.UpdatePDFPageCount(userID, id, *body.PageCount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		doc.PageCount = *body.PageCount
	}

	writeJSON(w, http.StatusOK, doc)
}

// handleStreamPDFContent streams the raw PDF binary content with byte-range support.
func (s *Server) handleStreamPDFContent(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	filePath, doc, err := s.db.GetPDFFilePath(userID, id)
	if err != nil || doc == nil {
		writeError(w, http.StatusNotFound, "pdf document not found")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", doc.OriginalName))
	http.ServeFile(w, r, filePath)
}

// handleDeletePDF deletes a PDF document and all its highlights.
func (s *Server) handleDeletePDF(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	err := s.db.DeletePDF(userID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleListPDFHighlights lists all highlights for a given PDF.
func (s *Server) handleListPDFHighlights(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")
	highlights, err := s.db.ListPDFHighlights(userID, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, highlights)
}

// handleCreatePDFHighlight creates a new visual highlight annotation.
func (s *Server) handleCreatePDFHighlight(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	id := r.PathValue("id")

	var body struct {
		PageNumber  int             `json:"page_number"`
		RectsJSON   json.RawMessage `json:"rects_json"`
		TextContent string          `json:"text_content"`
		Color       string          `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(body.TextContent) == "" {
		writeError(w, http.StatusBadRequest, "text_content cannot be empty")
		return
	}

	rectsStr := "[]"
	if len(body.RectsJSON) > 0 {
		var s string
		if err := json.Unmarshal(body.RectsJSON, &s); err == nil {
			rectsStr = s
		} else {
			rectsStr = string(body.RectsJSON)
		}
	}

	if !json.Valid([]byte(rectsStr)) {
		writeError(w, http.StatusBadRequest, "invalid rects_json: must be valid JSON array")
		return
	}

	hl, err := s.db.CreatePDFHighlight(userID, id, body.PageNumber, rectsStr, body.TextContent, body.Color)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, hl)
}

// handleDeletePDFHighlight deletes a highlight annotation.
func (s *Server) handleDeletePDFHighlight(w http.ResponseWriter, r *http.Request) {
	userID := s.getUserID(r)
	hlID := r.PathValue("hl_id")
	pdfID := ""
	if hlID != "" {
		// Route: DELETE /api/pdfs/{id}/highlights/{hl_id}
		pdfID = r.PathValue("id")
	} else {
		// Route: DELETE /api/highlights/{id}
		hlID = r.PathValue("id")
	}

	if hlID == "" {
		writeError(w, http.StatusBadRequest, "missing highlight id")
		return
	}

	err := s.db.DeletePDFHighlightForPDF(userID, pdfID, hlID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
