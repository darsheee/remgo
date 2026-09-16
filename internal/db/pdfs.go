package db

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PDFDocument represents an uploaded PDF file with metadata and page counts.
type PDFDocument struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	Filename     string    `json:"filename"`
	OriginalName string    `json:"original_name"`
	FileSize     int64     `json:"file_size"`
	PageCount    int       `json:"page_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PDFHighlight represents an excerpt annotation on a specific page of a PDF.
type PDFHighlight struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id,omitempty"`
	PDFID       string    `json:"pdf_id"`
	PageNumber  int       `json:"page_number"`
	RectsJSON   string    `json:"rects_json"`
	TextContent string    `json:"text_content"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
	PinRef      string    `json:"pin_ref,omitempty"`
}

// computePinRef formats a canonical pin reference string for a highlight.
func computePinRef(pdfID string, pageNumber int, highlightID string) string {
	return fmt.Sprintf("[[pdf:%s#p=%d&h=%s|p.%d]]", pdfID, pageNumber, highlightID, pageNumber)
}

var (
	// Regex matching /Type /Pages dictionaries with /Count N (order 1: Type then Count)
	pdfPagesCountRegex1 = regexp.MustCompile(`(?s)/Type\s*/Pages[^>]*?/Count\s+(\d+)`)
	// Regex matching /Type /Pages dictionaries with /Count N (order 2: Count then Type)
	pdfPagesCountRegex2 = regexp.MustCompile(`(?s)/Count\s+(\d+)[^>]*?/Type\s*/Pages`)
	// Fallback regex matching general /Count N in PDF objects
	pdfCountRegex = regexp.MustCompile(`/Count\s+(\d+)`)
	// Fallback regex matching individual /Type /Page objects (excluding /Pages)
	pdfPageTypeRegex = regexp.MustCompile(`/Type\s*/Page\b`)
)

// CountPDFPages inspects raw PDF bytes and extracts the page count.
func CountPDFPages(data []byte) int {
	maxCount := 0

	// 1. Primary: search for /Type /Pages ... /Count N in either dictionary order
	matches1 := pdfPagesCountRegex1.FindAllSubmatch(data, -1)
	for _, m := range matches1 {
		if len(m) > 1 {
			if cnt, err := strconv.Atoi(string(m[1])); err == nil && cnt > maxCount {
				maxCount = cnt
			}
		}
	}
	matches2 := pdfPagesCountRegex2.FindAllSubmatch(data, -1)
	for _, m := range matches2 {
		if len(m) > 1 {
			if cnt, err := strconv.Atoi(string(m[1])); err == nil && cnt > maxCount {
				maxCount = cnt
			}
		}
	}
	if maxCount > 0 {
		return maxCount
	}

	// 2. Secondary: search for any /Count N
	countMatches := pdfCountRegex.FindAllSubmatch(data, -1)
	for _, m := range countMatches {
		if len(m) > 1 {
			if cnt, err := strconv.Atoi(string(m[1])); err == nil && cnt > maxCount {
				maxCount = cnt
			}
		}
	}
	if maxCount > 0 {
		return maxCount
	}

	// 3. Fallback: count occurrences of /Type /Page
	pageMatches := pdfPageTypeRegex.FindAll(data, -1)
	if len(pageMatches) > 0 {
		return len(pageMatches)
	}

	// Default to at least 1 page for valid PDF documents
	return 1
}

// SavePDF validates, stores a PDF to the data directory, and registers it in SQLite.
func (d *DB) SavePDF(userID string, originalName string, r io.Reader) (*PDFDocument, error) {
	if userID == "" {
		userID = DefaultUserID
	}

	// Limit upload size to 100 MB to prevent resource exhaustion
	const maxUploadSize = 100 * 1024 * 1024
	lr := io.LimitReader(r, maxUploadSize+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF data: %w", err)
	}
	if len(data) > maxUploadSize {
		return nil, fmt.Errorf("PDF file size exceeds maximum limit of 100MB")
	}

	// Validate PDF header magic bytes (must contain %PDF- in first 1024 bytes)
	checkHeaderLen := 1024
	if len(data) < checkHeaderLen {
		checkHeaderLen = len(data)
	}
	if !bytes.Contains(data[:checkHeaderLen], []byte("%PDF-")) {
		return nil, fmt.Errorf("invalid file: missing standard %%PDF- header signature")
	}

	cleanName := filepath.Base(strings.TrimSpace(originalName))
	if cleanName == "" || cleanName == "." || cleanName == "/" {
		cleanName = "document.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(cleanName), ".pdf") {
		cleanName += ".pdf"
	}

	id := "pdf_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	filename := id + ".pdf"
	pageCount := CountPDFPages(data)
	fileSize := int64(len(data))
	now := time.Now().UTC()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Ensure PDF storage directory exists
	if err := os.MkdirAll(d.pdfDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pdf directory: %w", err)
	}

	targetPath := filepath.Join(d.pdfDir, filename)
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write PDF file to disk: %w", err)
	}

	_, err = d.sqlDB.Exec(
		`INSERT INTO pdfs(id, user_id, filename, original_name, file_size, page_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, filename, cleanName, fileSize, pageCount, now, now,
	)
	if err != nil {
		// Clean up disk file on DB insert failure
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("failed to insert pdf record: %w", err)
	}

	return &PDFDocument{
		ID:           id,
		UserID:       userID,
		Filename:     filename,
		OriginalName: cleanName,
		FileSize:     fileSize,
		PageCount:    pageCount,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// GetPDF retrieves a PDF document record by ID for a user.
func (d *DB) GetPDF(userID string, pdfID string) (*PDFDocument, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	var doc PDFDocument
	err := d.sqlDB.QueryRow(
		`SELECT id, user_id, filename, original_name, file_size, page_count, created_at, updated_at
		 FROM pdfs WHERE id = ? AND user_id = ?`,
		pdfID, userID,
	).Scan(
		&doc.ID, &doc.UserID, &doc.Filename, &doc.OriginalName,
		&doc.FileSize, &doc.PageCount, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query pdf: %w", err)
	}
	return &doc, nil
}

// ListPDFs retrieves all PDF documents for a user, sorted by update date descending.
func (d *DB) ListPDFs(userID string) ([]*PDFDocument, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.sqlDB.Query(
		`SELECT id, user_id, filename, original_name, file_size, page_count, created_at, updated_at
		 FROM pdfs WHERE user_id = ? ORDER BY updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list pdfs: %w", err)
	}
	defer rows.Close()

	var list []*PDFDocument
	for rows.Next() {
		var doc PDFDocument
		if err := rows.Scan(
			&doc.ID, &doc.UserID, &doc.Filename, &doc.OriginalName,
			&doc.FileSize, &doc.PageCount, &doc.CreatedAt, &doc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan pdf row: %w", err)
		}
		list = append(list, &doc)
	}
	if list == nil {
		list = []*PDFDocument{}
	}
	return list, nil
}

// GetPDFFilePath returns the filesystem path and metadata of a PDF for streaming.
func (d *DB) GetPDFFilePath(userID string, pdfID string) (string, *PDFDocument, error) {
	doc, err := d.GetPDF(userID, pdfID)
	if err != nil {
		return "", nil, err
	}
	if doc == nil {
		return "", nil, fmt.Errorf("pdf document not found")
	}

	d.mu.RLock()
	fullPath := filepath.Join(d.pdfDir, doc.Filename)
	d.mu.RUnlock()

	if _, err := os.Stat(fullPath); err != nil {
		return "", nil, fmt.Errorf("pdf file missing from storage: %w", err)
	}
	return fullPath, doc, nil
}

// DeletePDF removes a PDF document and all associated highlights from the DB and disk.
func (d *DB) DeletePDF(userID string, pdfID string) error {
	doc, err := d.GetPDF(userID, pdfID)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("pdf document not found")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.sqlDB.Exec("DELETE FROM pdfs WHERE id = ? AND user_id = ?", pdfID, userID)
	if err != nil {
		return fmt.Errorf("failed to delete pdf record: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("pdf document not found or access denied")
	}

	// Remove physical file from disk
	filePath := filepath.Join(d.pdfDir, doc.Filename)
	_ = os.Remove(filePath)

	return nil
}

// UpdatePDFPageCount updates the recorded page count for a document.
func (d *DB) UpdatePDFPageCount(userID string, pdfID string, pageCount int) error {
	if userID == "" {
		userID = DefaultUserID
	}
	if pageCount < 1 {
		pageCount = 1
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.sqlDB.Exec(
		"UPDATE pdfs SET page_count = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		pageCount, time.Now().UTC(), pdfID, userID,
	)
	return err
}

// CreatePDFHighlight records a new visual highlight/excerpt for a PDF document.
func (d *DB) CreatePDFHighlight(userID string, pdfID string, pageNumber int, rectsJSON string, textContent string, color string) (*PDFHighlight, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	if pageNumber < 1 {
		pageNumber = 1
	}
	if strings.TrimSpace(color) == "" {
		color = "#ffeb3b"
	}
	if strings.TrimSpace(rectsJSON) == "" {
		rectsJSON = "[]"
	}

	// Verify document exists and belongs to user
	doc, err := d.GetPDF(userID, pdfID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("pdf document not found: %s", pdfID)
	}

	id := "hl_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	now := time.Now().UTC()

	d.mu.Lock()
	defer d.mu.Unlock()

	_, err = d.sqlDB.Exec(
		`INSERT INTO pdf_highlights(id, user_id, pdf_id, page_number, rects_json, text_content, color, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, pdfID, pageNumber, rectsJSON, textContent, color, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert pdf highlight: %w", err)
	}

	return &PDFHighlight{
		ID:          id,
		UserID:      userID,
		PDFID:       pdfID,
		PageNumber:  pageNumber,
		RectsJSON:   rectsJSON,
		TextContent: textContent,
		Color:       color,
		CreatedAt:   now,
		PinRef:      computePinRef(pdfID, pageNumber, id),
	}, nil
}

// GetPDFHighlight retrieves a specific highlight by ID.
func (d *DB) GetPDFHighlight(userID string, highlightID string) (*PDFHighlight, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	var hl PDFHighlight
	err := d.sqlDB.QueryRow(
		`SELECT id, user_id, pdf_id, page_number, rects_json, text_content, color, created_at
		 FROM pdf_highlights WHERE id = ? AND user_id = ?`,
		highlightID, userID,
	).Scan(
		&hl.ID, &hl.UserID, &hl.PDFID, &hl.PageNumber, &hl.RectsJSON,
		&hl.TextContent, &hl.Color, &hl.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query highlight: %w", err)
	}
	hl.PinRef = computePinRef(hl.PDFID, hl.PageNumber, hl.ID)
	return &hl, nil
}

// ListPDFHighlights retrieves all highlights for a given PDF document.
func (d *DB) ListPDFHighlights(userID string, pdfID string) ([]*PDFHighlight, error) {
	if userID == "" {
		userID = DefaultUserID
	}
	// Verify document exists and belongs to user
	doc, err := d.GetPDF(userID, pdfID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("pdf document not found: %s", pdfID)
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.sqlDB.Query(
		`SELECT id, user_id, pdf_id, page_number, rects_json, text_content, color, created_at
		 FROM pdf_highlights WHERE pdf_id = ? AND user_id = ?
		 ORDER BY page_number ASC, created_at ASC`,
		pdfID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list pdf highlights: %w", err)
	}
	defer rows.Close()

	var list []*PDFHighlight
	for rows.Next() {
		var hl PDFHighlight
		if err := rows.Scan(
			&hl.ID, &hl.UserID, &hl.PDFID, &hl.PageNumber, &hl.RectsJSON,
			&hl.TextContent, &hl.Color, &hl.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan highlight row: %w", err)
		}
		hl.PinRef = computePinRef(hl.PDFID, hl.PageNumber, hl.ID)
		list = append(list, &hl)
	}
	if list == nil {
		list = []*PDFHighlight{}
	}
	return list, nil
}

// DeletePDFHighlight removes a highlight annotation by highlight ID.
func (d *DB) DeletePDFHighlight(userID string, highlightID string) error {
	return d.DeletePDFHighlightForPDF(userID, "", highlightID)
}

// DeletePDFHighlightForPDF removes a highlight annotation, verifying PDF ownership if pdfID is provided.
func (d *DB) DeletePDFHighlightForPDF(userID string, pdfID string, highlightID string) error {
	if userID == "" {
		userID = DefaultUserID
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	var query string
	var args []interface{}
	if pdfID != "" {
		query = "DELETE FROM pdf_highlights WHERE id = ? AND pdf_id = ? AND user_id = ?"
		args = []interface{}{highlightID, pdfID, userID}
	} else {
		query = "DELETE FROM pdf_highlights WHERE id = ? AND user_id = ?"
		args = []interface{}{highlightID, userID}
	}

	res, err := d.sqlDB.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete highlight: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("highlight not found or access denied")
	}
	return nil
}
