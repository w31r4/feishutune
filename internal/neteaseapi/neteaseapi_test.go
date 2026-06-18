package neteaseapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
	"github.com/Durden-T/feishutune/internal/neteaseauth"
)

func TestClientDisabledWithoutCredentials(t *testing.T) {
	c := New(neteaseauth.Credentials{})
	if c.Enabled() {
		t.Fatal("Enabled with empty credentials = true, want false")
	}
	got, err := c.Enhance(context.Background(), bio.Track{Playing: true, Name: "Song", Artist: "Artist"})
	if err != nil {
		t.Fatalf("Enhance disabled: %v", err)
	}
	if got.Name != "Song" {
		t.Fatalf("Enhance disabled = %+v, want original", got)
	}
	if liked, ok, err := c.LikedStatus(context.Background(), bio.Track{ID: "netease:track:1"}); liked || ok || err != nil {
		t.Fatalf("LikedStatus disabled = (%v, %v, %v), want (false, false, nil)", liked, ok, err)
	}
}

func TestSongDetailSendsSignedRequestAndMapsTrack(t *testing.T) {
	key, priv := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/detail" {
			t.Fatalf("path = %s, want /detail", r.URL.Path)
		}
		verifySignedQuery(t, r.URL.Query(), &key.PublicKey)
		var biz map[string]string
		if err := json.Unmarshal([]byte(r.URL.Query().Get("bizContent")), &biz); err != nil {
			t.Fatalf("bizContent: %v", err)
		}
		if biz["songId"] != "2725880283" {
			t.Fatalf("songId = %q", biz["songId"])
		}
		w.Write([]byte(`{"code":200,"data":{"song":{"id":"2725880283","name":"标准名","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000}}}`))
	}))
	defer srv.Close()

	c := testClient(srv.URL, priv, Endpoints{SongDetail: "/detail"})
	got, err := c.SongDetail(context.Background(), "2725880283")
	if err != nil {
		t.Fatalf("SongDetail: %v", err)
	}
	want := bio.Track{ID: "netease:track:2725880283", Name: "标准名", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second}
	if got != want {
		t.Fatalf("SongDetail = %+v, want %+v", got, want)
	}
}

func TestEnhanceUsesDetailWhenTrackHasID(t *testing.T) {
	_, priv := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":200,"data":{"id":"2725880283","name":"标准名","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000}}`))
	}))
	defer srv.Close()

	c := testClient(srv.URL, priv, Endpoints{SongDetail: "/detail"})
	got, err := c.Enhance(context.Background(), bio.Track{
		Playing:  true,
		ID:       "netease:track:2725880283",
		Name:     "原名",
		Artist:   "原歌手",
		Position: 7 * time.Second,
	})
	if err != nil {
		t.Fatalf("Enhance: %v", err)
	}
	if got.Name != "标准名" || got.Artist != "驼儿" || got.Album != "眠气" || got.Duration != 203*time.Second {
		t.Fatalf("Enhance = %+v, want standard metadata", got)
	}
	if !got.Playing || got.Position != 7*time.Second {
		t.Fatalf("Enhance lost playback fields: %+v", got)
	}
}

func TestResolveByStrictSearch(t *testing.T) {
	_, priv := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":200,"data":{"songs":[
			{"id":"1","name":"下学路","artists":[{"name":"别人"}],"album":{"name":"眠气"},"duration":203000},
			{"id":"2725880283","name":"下学路","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000},
			{"id":"3","name":"下学路","artists":[{"name":"驼儿"}],"album":{"name":"另一张"},"duration":203000}
		]}}`))
	}))
	defer srv.Close()

	c := testClient(srv.URL, priv, Endpoints{Search: "/search"})
	got, err := c.Resolve(context.Background(), bio.Track{Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != "netease:track:2725880283" {
		t.Fatalf("Resolve ID = %q", got.ID)
	}
}

func TestResolveRejectsAmbiguousAndLooseMatches(t *testing.T) {
	_, priv := testKey(t)
	tests := []struct {
		name string
		body string
	}{
		{
			name: "ambiguous",
			body: `{"code":200,"data":{"songs":[
				{"id":"1","name":"下学路","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000},
				{"id":"2","name":"下学路","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000}
			]}}`,
		},
		{
			name: "requires album or duration agreement",
			body: `{"code":200,"data":{"songs":[
				{"id":"1","name":"下学路","artists":[{"name":"驼儿"}]}
			]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c := testClient(srv.URL, priv, Endpoints{Search: "/search"})
			if _, err := c.Resolve(context.Background(), bio.Track{Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second}); err == nil {
				t.Fatal("Resolve succeeded, want strict-match error")
			}
		})
	}
}

func TestLikedStatus(t *testing.T) {
	_, priv := testKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":200,"data":{"songs":[
			{"id":"2725880283","name":"下学路","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000}
		]}}`))
	}))
	defer srv.Close()
	c := testClient(srv.URL, priv, Endpoints{LikedSongs: "/liked"})

	liked, ok, err := c.LikedStatus(context.Background(), bio.Track{ID: "netease:track:2725880283"})
	if err != nil || !ok || !liked {
		t.Fatalf("LikedStatus liked = (%v, %v, %v), want (true, true, nil)", liked, ok, err)
	}
	liked, ok, err = c.LikedStatus(context.Background(), bio.Track{ID: "netease:track:999"})
	if err != nil || !ok || liked {
		t.Fatalf("LikedStatus miss = (%v, %v, %v), want (false, true, nil)", liked, ok, err)
	}
}

func TestHTTPAndMalformedResponsesReturnErrors(t *testing.T) {
	_, priv := testKey(t)
	for _, body := range []string{`not json`, `{"code":429,"message":"limit"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		c := testClient(srv.URL, priv, Endpoints{Search: "/search"})
		if _, err := c.SearchSongs(context.Background(), "x"); err == nil {
			t.Fatalf("SearchSongs(%s) succeeded, want error", body)
		}
		srv.Close()
	}
}

func testClient(baseURL, privateKey string, endpoints Endpoints) *Client {
	return New(neteaseauth.Credentials{
		AppID:       "app",
		PrivateKey:  privateKey,
		AccessToken: "token",
	}, WithBaseURL(baseURL), WithEndpoints(endpoints), WithNow(func() time.Time {
		return time.Unix(1_779_999_999, 0)
	}))
}

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, string(pemBytes)
}

func verifySignedQuery(t *testing.T, q url.Values, pub *rsa.PublicKey) {
	t.Helper()
	for _, key := range []string{"appId", "accessToken", "signType", "timestamp", "device", "bizContent", "sign"} {
		if strings.TrimSpace(q.Get(key)) == "" {
			t.Fatalf("missing query %s in %v", key, q)
		}
	}
	if q.Get("signType") != signType {
		t.Fatalf("signType = %q, want %q", q.Get("signType"), signType)
	}
	sig, err := base64.StdEncoding.DecodeString(q.Get("sign"))
	if err != nil {
		t.Fatalf("decode sign: %v", err)
	}
	unsigned := url.Values{}
	for k, vals := range q {
		if k != "sign" {
			unsigned[k] = append([]string(nil), vals...)
		}
	}
	sum := sha256.Sum256([]byte(canonical(unsigned)))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature verify: %v", err)
	}
}
