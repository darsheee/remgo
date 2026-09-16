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

func TestAPIPDFUpdateAndScopedHighlightDelete(t *testing.T) {
	server, database, tmpDir := setupTestAPIServer(t)
	defer os.RemoveAll(tmpDir)
	defer database.Close()

	handler := server.Handler()

	// 1. Upload PDF
	pdfBytes := makeTestPDFBytes(3)
	rawReq := httptest.NewRequest("POST", "/api/pdfs?filename=Physics.pdf", bytes.NewReader(pdfBytes))
	rawReq.Header.Set("Content-Type", "application/pdf")
	rawW := httptest.NewRecorder()
	handler.ServeHTTP(rawW, rawReq)
	if rawW.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rawW.Code)
	}
	var doc db.PDFDocument
	_ = json.Unmarshal(rawW.Body.Bytes(), &doc)

	// 2. PATCH /api/pdfs/{id} - update page count
	patchPayload := map[string]interface{}{"page_count": 15}
	patchJSON, _ := json.Marshal(patchPayload)
	patchReq := httptest.NewRequest("PATCH", "/api/pdfs/"+doc.ID, bytes.NewReader(patchJSON))
	patchReq.Header.Set("Content-Type", "application/json")
	patchW := httptest.NewRecorder()
	handler.ServeHTTP(patchW, patchReq)

	if patchW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on PATCH, got %d: %s", patchW.Code, patchW.Body.String())
	}
	var patchedDoc db.PDFDocument
	_ = json.Unmarshal(patchW.Body.Bytes(), &patchedDoc)
	if patchedDoc.PageCount != 15 {
		t.Fatalf("expected page count 15, got %d", patchedDoc.PageCount)
	}

	// 3. PUT /api/pdfs/{id} - update page count
	putPayload := map[string]interface{}{"page_count": 25}
	putJSON, _ := json.Marshal(putPayload)
	putReq := httptest.NewRequest("PUT", "/api/pdfs/"+doc.ID, bytes.NewReader(putJSON))
	putReq.Header.Set("Content-Type", "application/json")
	putW := httptest.NewRecorder()
	handler.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on PUT, got %d: %s", putW.Code, putW.Body.String())
	}
	var putDoc db.PDFDocument
	_ = json.Unmarshal(putW.Body.Bytes(), &putDoc)
	if putDoc.PageCount != 25 {
		t.Fatalf("expected page count 25, got %d", putDoc.PageCount)
	}

	// 4. Create Highlight
	hlPayload := map[string]interface{}{
		"page_number":  2,
		"rects_json":   `[{"x":0.2,"y":0.3,"w":0.4,"h":0.08}]`,
		"text_content": "Newtonian mechanics holds for low velocities",
		"color":        "#fed7aa",
	}
	hlJSON, _ := json.Marshal(hlPayload)
	createHlReq := httptest.NewRequest("POST", "/api/pdfs/"+doc.ID+"/highlights", bytes.NewReader(hlJSON))
	createHlReq.Header.Set("Content-Type", "application/json")
	createHlW := httptest.NewRecorder()
	handler.ServeHTTP(createHlW, createHlReq)

	if createHlW.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on highlight, got %d: %s", createHlW.Code, createHlW.Body.String())
	}
	var createdHl db.PDFHighlight
	_ = json.Unmarshal(createHlW.Body.Bytes(), &createdHl)

	// 5. Scoped Highlight Deletion with mismatched PDF ID -> must return 404
	delMismatchReq := httptest.NewRequest("DELETE", "/api/pdfs/wrong_doc_id/highlights/"+createdHl.ID, nil)
	delMismatchW := httptest.NewRecorder()
	handler.ServeHTTP(delMismatchW, delMismatchReq)
	if delMismatchW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found on mismatched pdf_id, got %d", delMismatchW.Code)
	}

	// 6. Scoped Highlight Deletion with matching PDF ID -> must return 200 OK
	delScopedReq := httptest.NewRequest("DELETE", "/api/pdfs/"+doc.ID+"/highlights/"+createdHl.ID, nil)
	delScopedW := httptest.NewRecorder()
	handler.ServeHTTP(delScopedW, delScopedReq)
	if delScopedW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on scoped delete, got %d: %s", delScopedW.Code, delScopedW.Body.String())
	}

	// 7. Verify highlight no longer exists
	delAgainReq := httptest.NewRequest("DELETE", "/api/pdfs/"+doc.ID+"/highlights/"+createdHl.ID, nil)
	delAgainW := httptest.NewRecorder()
	handler.ServeHTTP(delAgainW, delAgainReq)
	if delAgainW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found on already deleted highlight, got %d", delAgainW.Code)
	}
}

func TestAPIPDFValidationAndIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "remgo_api_pdf_iso_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Auth enabled server
	server := NewServer(database, nil, true)
	handler := server.Handler()

	alice, err := database.CreateUser("alice_api", "alice@test.com", "pass123", "user")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := database.CreateUser("bob_api", "bob@test.com", "pass123", "user")
	if err != nil {
		t.Fatal(err)
	}

	aliceToken, _, err := database.CreateAPIKey(alice.ID, "alice_key", nil)
	if err != nil {
		t.Fatal(err)
	}
	bobToken, _, err := database.CreateAPIKey(bob.ID, "bob_key", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Alice uploads PDF
	pdfBytes := makeTestPDFBytes(2)
	req := httptest.NewRequest("POST", "/api/pdfs?filename=Alice_Private.pdf", bytes.NewReader(pdfBytes))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/pdf")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Alice upload failed: %d, %s", w.Code, w.Body.String())
	}
	var aliceDoc db.PDFDocument
	_ = json.Unmarshal(w.Body.Bytes(), &aliceDoc)

	// 2. Bob cannot view Alice's PDF
	bobGetReq := httptest.NewRequest("GET", "/api/pdfs/"+aliceDoc.ID, nil)
	bobGetReq.Header.Set("Authorization", "Bearer "+bobToken)
	bobGetW := httptest.NewRecorder()
	handler.ServeHTTP(bobGetW, bobGetReq)
	if bobGetW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Bob accessing Alice's PDF, got %d", bobGetW.Code)
	}

	// 3. Bob cannot stream Alice's PDF content
	bobStreamReq := httptest.NewRequest("GET", "/api/pdfs/"+aliceDoc.ID+"/content", nil)
	bobStreamReq.Header.Set("Authorization", "Bearer "+bobToken)
	bobStreamW := httptest.NewRecorder()
	handler.ServeHTTP(bobStreamW, bobStreamReq)
	if bobStreamW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Bob streaming Alice's PDF, got %d", bobStreamW.Code)
	}

	// 4. Bob cannot create highlight on Alice's PDF
	hlPayload := map[string]interface{}{
		"page_number":  1,
		"rects_json":   `[]`,
		"text_content": "Bob intrusion",
		"color":        "#ffeb3b",
	}
	hlJSON, _ := json.Marshal(hlPayload)
	bobHlReq := httptest.NewRequest("POST", "/api/pdfs/"+aliceDoc.ID+"/highlights", bytes.NewReader(hlJSON))
	bobHlReq.Header.Set("Authorization", "Bearer "+bobToken)
	bobHlReq.Header.Set("Content-Type", "application/json")
	bobHlW := httptest.NewRecorder()
	handler.ServeHTTP(bobHlW, bobHlReq)
	if bobHlW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Bob adding highlight to Alice's PDF, got %d", bobHlW.Code)
	}

	// 5. Validation: empty text_content must return 400
	badHlPayload1 := map[string]interface{}{
		"page_number":  1,
		"text_content": "   ",
	}
	badJSON1, _ := json.Marshal(badHlPayload1)
	badReq1 := httptest.NewRequest("POST", "/api/pdfs/"+aliceDoc.ID+"/highlights", bytes.NewReader(badJSON1))
	badReq1.Header.Set("Authorization", "Bearer "+aliceToken)
	badReq1.Header.Set("Content-Type", "application/json")
	badW1 := httptest.NewRecorder()
	handler.ServeHTTP(badW1, badReq1)
	if badW1.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on empty text_content, got %d", badW1.Code)
	}

	// 6. Validation: invalid JSON for rects_json must return 400
	badHlPayload2 := map[string]interface{}{
		"page_number":  1,
		"rects_json":   `not-valid-json`,
		"text_content": "Some text",
	}
	badJSON2, _ := json.Marshal(badHlPayload2)
	badReq2 := httptest.NewRequest("POST", "/api/pdfs/"+aliceDoc.ID+"/highlights", bytes.NewReader(badJSON2))
	badReq2.Header.Set("Authorization", "Bearer "+aliceToken)
	badReq2.Header.Set("Content-Type", "application/json")
	badW2 := httptest.NewRecorder()
	handler.ServeHTTP(badW2, badReq2)
	if badW2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on invalid rects_json, got %d", badW2.Code)
	}
}

