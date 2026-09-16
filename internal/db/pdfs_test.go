package db

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Minimal valid PDF binary generator for testing
func makeTestPDF(pageCount int) []byte {
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

func TestCountPDFPages(t *testing.T) {
	// Case 1: 3-page standard PDF
	pdf3 := makeTestPDF(3)
	if count := CountPDFPages(pdf3); count != 3 {
		t.Fatalf("expected 3 pages, got %d", count)
	}

	// Case 2: 12-page PDF
	pdf12 := makeTestPDF(12)
	if count := CountPDFPages(pdf12); count != 12 {
		t.Fatalf("expected 12 pages, got %d", count)
	}

	// Case 3: Fallback with only /Type /Page
	rawFallback := []byte("%PDF-1.4\n/Type /Page\n/Type /Page\n/Type /Page\n%%EOF")
	if count := CountPDFPages(rawFallback); count != 3 {
		t.Fatalf("expected 3 pages from fallback, got %d", count)
	}

	// Case 4: Minimal valid PDF without count defaults to 1
	rawSingle := []byte("%PDF-1.4\nsome content\n%%EOF")
	if count := CountPDFPages(rawSingle); count != 1 {
		t.Fatalf("expected 1 page default, got %d", count)
	}
}

func TestPDFStorageAndHighlights(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "remgo_pdf_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	alice, err := database.CreateUser("alice", "alice@example.com", "pass123", "admin")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := database.CreateUser("bob", "bob@example.com", "pass456", "user")
	if err != nil {
		t.Fatal(err)
	}
	userA := alice.ID
	userB := bob.ID

	// 1. Invalid PDF upload rejection
	invalidBytes := []byte("Not a real PDF file")
	_, err = database.SavePDF(userA, "fake.pdf", bytes.NewReader(invalidBytes))
	if err == nil || !strings.Contains(err.Error(), "%PDF-") {
		t.Fatalf("expected invalid PDF error, got: %v", err)
	}

	// 2. Valid PDF upload for User A
	pdfBytes := makeTestPDF(5)
	docA, err := database.SavePDF(userA, "Research_Paper.pdf", bytes.NewReader(pdfBytes))
	if err != nil {
		t.Fatalf("failed to save PDF: %v", err)
	}
	if docA.OriginalName != "Research_Paper.pdf" {
		t.Errorf("expected original name 'Research_Paper.pdf', got %s", docA.OriginalName)
	}
	if docA.PageCount != 5 {
		t.Errorf("expected 5 pages, got %d", docA.PageCount)
	}
	if docA.FileSize != int64(len(pdfBytes)) {
		t.Errorf("expected file size %d, got %d", len(pdfBytes), docA.FileSize)
	}

	// Verify file was written to disk
	filePath, fetchedDoc, err := database.GetPDFFilePath(userA, docA.ID)
	if err != nil {
		t.Fatalf("failed to get PDF file path: %v", err)
	}
	if fetchedDoc.ID != docA.ID {
		t.Errorf("expected fetched doc ID %s, got %s", docA.ID, fetchedDoc.ID)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("PDF file does not exist on disk: %v", err)
	}

	// 3. User B cannot see or access User A's PDF
	docForB, err := database.GetPDF(userB, docA.ID)
	if err != nil {
		t.Fatalf("unexpected error querying for User B: %v", err)
	}
	if docForB != nil {
		t.Fatal("User B should not have access to User A's PDF")
	}

	_, _, err = database.GetPDFFilePath(userB, docA.ID)
	if err == nil {
		t.Fatal("User B should not be able to get file path for User A's PDF")
	}

	listB, err := database.ListPDFs(userB)
	if err != nil {
		t.Fatalf("failed to list User B PDFs: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("expected 0 PDFs for User B, got %d", len(listB))
	}

	// 4. Create Highlights for User A
	rects := `[{"x1":0.1,"y1":0.2,"x2":0.8,"y2":0.25,"w":0.7,"h":0.05}]`
	hl1, err := database.CreatePDFHighlight(userA, docA.ID, 1, rects, "Mitochondria produce ATP through oxidative phosphorylation", "#ffeb3b")
	if err != nil {
		t.Fatalf("failed to create highlight 1: %v", err)
	}
	if hl1.PDFID != docA.ID || hl1.PageNumber != 1 {
		t.Errorf("highlight metadata mismatch: %+v", hl1)
	}

	hl2, err := database.CreatePDFHighlight(userA, docA.ID, 3, rects, "Glycolysis occurs in the cytoplasm", "#a7f3d0")
	if err != nil {
		t.Fatalf("failed to create highlight 2: %v", err)
	}

	// User B cannot create highlight on User A's PDF
	_, err = database.CreatePDFHighlight(userB, docA.ID, 1, rects, "Unauthorized highlight", "#ffeb3b")
	if err == nil {
		t.Fatal("User B should not be allowed to highlight User A's PDF")
	}

	// 5. List Highlights
	hlsA, err := database.ListPDFHighlights(userA, docA.ID)
	if err != nil {
		t.Fatalf("failed to list highlights: %v", err)
	}
	if len(hlsA) != 2 {
		t.Fatalf("expected 2 highlights for docA, got %d", len(hlsA))
	}
	if hlsA[0].PageNumber != 1 || hlsA[1].PageNumber != 3 {
		t.Errorf("highlights not ordered by page number: %v, %v", hlsA[0].PageNumber, hlsA[1].PageNumber)
	}

	// 6. Delete single highlight
	if err := database.DeletePDFHighlight(userA, hl1.ID); err != nil {
		t.Fatalf("failed to delete highlight: %v", err)
	}
	hlsAfterDel, err := database.ListPDFHighlights(userA, docA.ID)
	if err != nil || len(hlsAfterDel) != 1 {
		t.Fatalf("expected 1 highlight remaining, got %d (err: %v)", len(hlsAfterDel), err)
	}

	// 7. Delete PDF cascades to disk file and remaining highlights
	if err := database.DeletePDF(userA, docA.ID); err != nil {
		t.Fatalf("failed to delete PDF: %v", err)
	}
	// Verify file is gone from disk
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("PDF file was not removed from disk after deletion: %v", err)
	}
	// Verify highlight was cascade deleted
	hlCheck, err := database.GetPDFHighlight(userA, hl2.ID)
	if err != nil {
		t.Fatalf("error checking highlight: %v", err)
	}
	if hlCheck != nil {
		t.Errorf("highlight was not deleted with PDF: %+v", hlCheck)
	}
}
