package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceUploadRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "private.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	return req
}

func TestWorkspaceRejectsSymlinkEscape(t *testing.T) {
	for _, action := range []string{"read", "download", "list", "write", "mkdir", "delete", "upload"} {
		t.Run(action, func(t *testing.T) {
			base, outside := t.TempDir(), t.TempDir()
			secret := filepath.Join(outside, "private.txt")
			const sentinel = "private-outside-workspace"
			if err := os.WriteFile(secret, []byte(sentinel), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			s := &Server{m: &Manager{dir: base}}
			w := httptest.NewRecorder()
			switch action {
			case "read":
				s.wsRead(w, httptest.NewRequest("GET", "/api/workspace/read?path=escape/private.txt", nil))
			case "download":
				s.wsDownload(w, httptest.NewRequest("GET", "/api/workspace/download?path=escape/private.txt", nil))
			case "list":
				s.wsList(w, httptest.NewRequest("GET", "/api/workspace/list?path=escape", nil))
			case "write":
				s.wsWrite(w, httptest.NewRequest("POST", "/api/workspace/write", strings.NewReader(`{"path":"escape/private.txt","content":"replacement"}`)))
			case "mkdir":
				s.wsMkdir(w, httptest.NewRequest("POST", "/api/workspace/mkdir", strings.NewReader(`{"path":"escape/new-dir"}`)))
			case "delete":
				s.wsDelete(w, httptest.NewRequest("DELETE", "/api/workspace/delete?path=escape/private.txt", nil))
			case "upload":
				s.wsUpload(w, workspaceUploadRequest(t, "/api/workspace/upload?path=escape"))
			}
			if w.Code < 400 {
				t.Errorf("symlink escape returned %d", w.Code)
			}
			if strings.Contains(w.Body.String(), sentinel) {
				t.Error("response exposed a file outside the workspace")
			}
			got, err := os.ReadFile(secret)
			if err != nil || string(got) != sentinel {
				t.Errorf("outside file was changed: err=%v", err)
			}
			if _, err := os.Stat(filepath.Join(outside, "new-dir")); !os.IsNotExist(err) {
				t.Error("created a directory outside the workspace")
			}
		})
	}
}

func TestChatUploadRejectsSymlinkEscape(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "drafts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "drafts", "draft")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &Server{m: &Manager{dir: base}}
	w := httptest.NewRecorder()
	s.chatUpload(w, workspaceUploadRequest(t, "/api/chat/upload?scope=staging&id=draft"))
	if w.Code < 400 {
		t.Fatalf("symlink escape returned %d", w.Code)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("upload wrote outside workspace: entries=%d err=%v", len(entries), err)
	}
}

func TestWorkspaceKeepsNormalFilesAndContainedSymlinksUsable(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "safe"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "safe", "example.txt"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("safe", filepath.Join(base, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := &Server{m: &Manager{dir: base}}
	w := httptest.NewRecorder()
	s.wsRead(w, httptest.NewRequest("GET", "/api/workspace/read?path=alias/example.txt", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "visible") {
		t.Fatalf("contained file unreadable: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	s.wsWrite(w, httptest.NewRequest("POST", "/api/workspace/write", strings.NewReader(`{"path":"new/example.txt","content":"saved"}`)))
	if w.Code != 200 {
		t.Fatalf("normal write failed: %d", w.Code)
	}
	got, err := os.ReadFile(filepath.Join(base, "new", "example.txt"))
	if err != nil || string(got) != "saved" {
		t.Fatal("normal write did not save content")
	}
}

func TestChatUploadsDoNotOverwriteDuplicateNames(t *testing.T) {
	base := t.TempDir()
	s := &Server{m: &Manager{dir: base}}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		s.chatUpload(w, workspaceUploadRequest(t, "/api/chat/upload?scope=staging&id=draft"))
		if w.Code != 200 {
			t.Fatalf("upload %d failed: %d %s", i, w.Code, w.Body)
		}
	}
	entries, err := os.ReadDir(filepath.Join(base, "drafts", "draft", "uploads"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected two separate attachments: err=%v entries=%d", err, len(entries))
	}
}
