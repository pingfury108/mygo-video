package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const listing = `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/</d:href>
    <d:propstat><d:prop>
      <d:displayname>dav</d:displayname>
      <d:resourcetype><d:collection/></d:resourcetype>
    </d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/Movies/</d:href>
    <d:propstat><d:prop>
      <d:displayname>Movies</d:displayname>
      <d:resourcetype><d:collection/></d:resourcetype>
    </d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/Heat%20(1995).mkv</d:href>
    <d:propstat><d:prop>
      <d:displayname>Heat (1995).mkv</d:displayname>
      <d:resourcetype/>
      <d:getcontentlength>123</d:getcontentlength>
      <d:getlastmodified>Mon, 02 Jan 2006 15:04:05 GMT</d:getlastmodified>
    </d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
  </d:response>
</d:multistatus>`

func server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Depth") != "1" {
			http.Error(w, "depth", http.StatusBadRequest)
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "alex" || pass != "secret" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(listing))
	}))
}

func TestList(t *testing.T) {
	srv := server(t)
	defer srv.Close()
	c, err := New(Config{URL: srv.URL + "/dav", User: "alex", Pass: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := c.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries %+v", entries)
	}
	if entries[0].Path != "Movies" || !entries[0].Dir {
		t.Fatalf("folder %+v", entries[0])
	}
	if entries[1].Path != "Heat (1995).mkv" || entries[1].Dir || entries[1].Size != 123 {
		t.Fatalf("file %+v", entries[1])
	}
	if entries[1].ModTime.IsZero() {
		t.Fatal("no modified time")
	}
}

func TestListRejectsAuth(t *testing.T) {
	srv := server(t)
	defer srv.Close()
	c, err := New(Config{URL: srv.URL + "/dav", User: "alex", Pass: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(context.Background(), ""); err == nil {
		t.Fatal("a wrong password was accepted")
	}
}

func TestURL(t *testing.T) {
	c, err := New(Config{URL: "http://nas.local:5005/dav/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.URL(""); got != "http://nas.local:5005/dav/" {
		t.Fatalf("root %q", got)
	}
	if got := c.URL("Movies/Heat (1995).mkv"); got != "http://nas.local:5005/dav/Movies/Heat%20%281995%29.mkv" {
		t.Fatalf("file %q", got)
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	for _, raw := range []string{"", "nas.local/dav", "ftp://nas.local/dav"} {
		if _, err := New(Config{URL: raw}); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
}
