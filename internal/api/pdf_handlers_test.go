package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/darsheee/remgo/internal/db"
)

func makeTestPDFBytes(pageCount int) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Count %d /Kids [", pageCount))
	for i := 1; i <= pageCount; i++ {
		buf.WriteString(fmt.Sprintf(" %d 0 R", i+2))
	}
	buf.WriteString(" ] >>\nendobj\n")

	for i := 1; i <= pageCount; i++ {
		buf.WriteString(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n", i+2))
	}

	buf.WriteString("xref\n0 5\n0000000000 65535 f \n")
	buf.WriteString("trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n500\n%%EOF\n")
	return buf.Bytes()
}

func setupTestAPIServer(t *testing.T) (*Server, *db.DB, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "remgo_api_pdf_test_*")
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	server := NewServer(database, nil, false)
	return server, database, tmpDir
}

func TestAPIPDFWorkflow(t *testing.T) {
	server, database, tmpDir := setupTestAPIServer(t)
	defer os.RemoveAll(tmpDir)
	defer database.Close()

	handler := server.Handler()

	// 1. Upload PDF via multipart/form-data
	pdfBytes := makeTestPDFBytes(4)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "Bio_Textbook.pdf")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(pdfBytes)
	writer.Close()

	req := httptest.NewRequest("POST", "/api/pdfs", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on upload, got %d: %s", w.Code, w.Body.String())
	}

	var uploadedDoc db.PDFDocument
	if err := json.Unmarshal(w.Body.Bytes(), &uploadedDoc); err != nil {
		t.Fatalf("failed to parse uploaded doc response: %v", err)
	}
	if uploadedDoc.ID == "" || uploadedDoc.OriginalName != "Bio_Textbook.pdf" || uploadedDoc.PageCount != 4 {
		t.Fatalf("unexpected uploaded doc metadata: %+v", uploadedDoc)
	}

	// 2. Upload second PDF via raw binary
	rawPdfBytes := makeTestPDFBytes(2)
	req2 := httptest.NewRequest("POST", "/api/pdfs?filename=Notes.pdf", bytes.NewReader(rawPdfBytes))
	req2.Header.Set("Content-Type", "application/pdf")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on raw upload, got %d: %s", w2.Code, w2.Body.String())
	}

	// 3. List PDFs
	listReq := httptest.NewRequest("GET", "/api/pdfs", nil)
	listW := httptest.NewRecorder()
	handler.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on list, got %d: %s", listW.Code, listW.Body.String())
	}
	var list []db.PDFDocument
	if err := json.Unmarshal(listW.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 PDFs, got %d", len(list))
	}

	// 4. Get PDF metadata
	getReq := httptest.NewRequest("GET", "/api/pdfs/"+uploadedDoc.ID, nil)
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on get, got %d: %s", getW.Code, getW.Body.String())
	}

	// 5. Stream PDF Content (Full and Range requests)
	streamReq := httptest.NewRequest("GET", "/api/pdfs/"+uploadedDoc.ID+"/content", nil)
	streamW := httptest.NewRecorder()
	handler.ServeHTTP(streamW, streamReq)

	if streamW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on stream, got %d: %s", streamW.Code, streamW.Body.String())
	}
	if ct := streamW.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("expected Content-Type application/pdf, got %s", ct)
	}
	if streamW.Body.Len() != len(pdfBytes) {
		t.Errorf("expected %d bytes streamed, got %d", len(pdfBytes), streamW.Body.Len())
	}

	// Byte Range request
	rangeReq := httptest.NewRequest("GET", "/api/pdfs/"+uploadedDoc.ID+"/content", nil)
	rangeReq.Header.Set("Range", "bytes=0-7")
	rangeW := httptest.NewRecorder()
	handler.ServeHTTP(rangeW, rangeReq)

	if rangeW.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rangeW.Code)
	}
	if rangeW.Body.Len() != 8 {
		t.Fatalf("expected 8 bytes in range response, got %d", rangeW.Body.Len())
	}
	if string(rangeW.Body.Bytes()[:5]) != "%PDF-" {
		t.Errorf("expected '%%PDF-', got %s", rangeW.Body.Bytes()[:5])
	}

	// 6. Create Highlight
	hlPayload := map[string]interface{}{
		"page_number":  2,
		"rects_json":   `[{"x":0.1,"y":0.2,"w":0.5,"h":0.05}]`,
		"text_content": "Cellular respiration produces 32 ATP",
		"color":        "#a7f3d0",
	}
	hlJSON, _ := json.Marshal(hlPayload)
	createHlReq := httptest.NewRequest("POST", "/api/pdfs/"+uploadedDoc.ID+"/highlights", bytes.NewReader(hlJSON))
	createHlReq.Header.Set("Content-Type", "application/json")
	createHlW := httptest.NewRecorder()
	handler.ServeHTTP(createHlW, createHlReq)

	if createHlW.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on highlight, got %d: %s", createHlW.Code, createHlW.Body.String())
	}
	var createdHl db.PDFHighlight
	_ = json.Unmarshal(createHlW.Body.Bytes(), &createdHl)
	if createdHl.ID == "" || createdHl.PageNumber != 2 || createdHl.Color != "#a7f3d0" {
		t.Fatalf("unexpected highlight data: %+v", createdHl)
	}

	// 7. List Highlights
	listHlReq := httptest.NewRequest("GET", "/api/pdfs/"+uploadedDoc.ID+"/highlights", nil)
	listHlW := httptest.NewRecorder()
	handler.ServeHTTP(listHlW, listHlReq)

	if listHlW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on list highlights, got %d: %s", listHlW.Code, listHlW.Body.String())
	}
	var hls []db.PDFHighlight
	_ = json.Unmarshal(listHlW.Body.Bytes(), &hls)
	if len(hls) != 1 || hls[0].ID != createdHl.ID {
		t.Fatalf("expected 1 highlight, got %+v", hls)
	}

	// 8. Delete Highlight
	delHlReq := httptest.NewRequest("DELETE", "/api/highlights/"+createdHl.ID, nil)
	delHlW := httptest.NewRecorder()
	handler.ServeHTTP(delHlW, delHlReq)

	if delHlW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on delete highlight, got %d: %s", delHlW.Code, delHlW.Body.String())
	}

	// 9. Delete PDF
	delPdfReq := httptest.NewRequest("DELETE", "/api/pdfs/"+uploadedDoc.ID, nil)
	delPdfW := httptest.NewRecorder()
	handler.ServeHTTP(delPdfW, delPdfReq)

	if delPdfW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on delete pdf, got %d: %s", delPdfW.Code, delPdfW.Body.String())
	}

	// Verify 404 after deletion
	checkReq := httptest.NewRequest("GET", "/api/pdfs/"+uploadedDoc.ID, nil)
	checkW := httptest.NewRecorder()
	handler.ServeHTTP(checkW, checkReq)
	if checkW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found after delete, got %d", checkW.Code)
	}
}
