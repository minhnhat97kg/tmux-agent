package hook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostRemoteNtfyAndTelegram(t *testing.T) {
	var gotTitle, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotTitle, gotBody, gotPath = r.Header.Get("Title"), string(b), r.URL.Path
	}))
	defer srv.Close()

	if err := postRemote(srv.URL+"/topic", "api", "Task completed"); err != nil {
		t.Fatal(err)
	}
	if gotTitle != "api" || gotBody != "Task completed" {
		t.Errorf("ntfy: title %q body %q", gotTitle, gotBody)
	}
	if err := postRemote(srv.URL+"/api.telegram.org/bot123/sendMessage?chat_id=9", "api", "done"); err != nil {
		t.Fatal(err)
	}
	if gotBody != "text=api%0Adone" || gotPath != "/api.telegram.org/bot123/sendMessage" {
		t.Errorf("telegram: path %q body %q", gotPath, gotBody)
	}
}
