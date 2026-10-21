package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GetSetting 对"键不存在"和"读取出错"的返回值只差一个 error：两种情况 value 都是
// 空串。密码相关的 handler 一旦把 error 当成"还没设置密码"，就会在数据库抖动期间
// 敞开初始化入口——authInit 会放行一个未认证请求去覆盖已有的管理员密码，
// authStatus 则会把前端直接送到 /setup 去照着做这件事。
//
// 这两个测试把 handler 的连接池关掉来制造读取失败，断言两处都 fail closed。
func TestAuthStatusFailsClosedWhenDataSourceUnavailable(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v)", err)
	}
	defer m.Close()
	// 关掉池子，让后续 GetSetting 返回 error 而不是 sql.ErrNoRows。
	if err := m.pg.Close(); err != nil {
		t.Fatal(err)
	}

	s := &Server{m: m}
	w := httptest.NewRecorder()
	s.authStatus(w, httptest.NewRequest("GET", "/api/auth/status", nil))

	if w.Code != 503 {
		t.Fatalf("status=%d want 503 (读失败被当成未初始化会把用户送去 /setup 覆盖密码); body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Initialized *bool `json:"initialized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err == nil && payload.Initialized != nil {
		t.Fatalf("读失败时不应回答 initialized，得到 %v", *payload.Initialized)
	}
}

func TestAuthInitFailsClosedWhenDataSourceUnavailable(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v)", err)
	}
	defer m.Close()
	if err := m.pg.Close(); err != nil {
		t.Fatal(err)
	}

	s := &Server{m: m}
	w := httptest.NewRecorder()
	body := strings.NewReader(`{"password":"correct horse battery"}`)
	s.authInit(w, httptest.NewRequest("POST", "/api/auth/init", body))

	if w.Code != 503 {
		t.Fatalf("status=%d want 503 (读失败时放行会让未认证请求覆盖已有密码); body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "token") {
		t.Fatalf("读失败时不应签发 token: %s", w.Body.String())
	}
}

func TestValidatePassword(t *testing.T) {
	for _, tc := range []struct {
		name, pw string
		wantErr  bool
	}{
		{"空", "", true},
		{"七位", "1234567", true},
		{"八位", "12345678", false},
		{"八个汉字按字符数而非字节数计", "密码密码密码密码", false},
		{"三个汉字够 9 字节但只有 3 个字符", "密码强", true},
		{"72 字节", strings.Repeat("a", 72), false},
		{"73 字节超出 bcrypt 上限", strings.Repeat("a", 73), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validatePassword(tc.pw); (got != "") != tc.wantErr {
				t.Fatalf("validatePassword(%q)=%q, wantErr=%v", tc.pw, got, tc.wantErr)
			}
		})
	}
}

func TestJWTRequiresExpectedAlgorithmSubjectAndExpiry(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		claims jwt.RegisteredClaims
		want   bool
	}{
		{"valid", jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "ARTEX", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, true},
		{"missing-expiry", jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "ARTEX"}, false},
		{"wrong-subject", jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "another-service", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, false},
		{"wrong-algorithm", jwt.SigningMethodHS512, jwt.RegisteredClaims{Subject: "ARTEX", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, false},
		{"expired", jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "ARTEX", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(tc.method, tc.claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if got := verifyJWT(token, key); got != tc.want {
				t.Fatalf("accepted=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestLoginAttemptsUsePeerAddressNotForwardedHeaders(t *testing.T) {
	s := &Server{}
	handler := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for i := 0; i < 11; i++ {
		r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
		r.RemoteAddr = fmt.Sprintf("192.0.2.10:%d", 10000+i)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("192.0.2.%d", i+20))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 204
		if i == 10 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: status=%d want=%d", i+1, w.Code, want)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("auth response can be cached")
		}
	}
}

func TestAuthAttemptWindowExpiresAndStorageIsBounded(t *testing.T) {
	now := time.Now()
	limiter := &authAttempts{peers: make(map[string]authWindow)}
	for i := 0; i < 10; i++ {
		if !limiter.allow("first", now) {
			t.Fatal("initial attempt blocked")
		}
	}
	if limiter.allow("first", now) {
		t.Fatal("exhausted bucket allowed")
	}
	if !limiter.allow("first", now.Add(16*time.Minute)) {
		t.Fatal("expired bucket remained blocked")
	}
	for i := 0; i < 5000; i++ {
		limiter.allow(fmt.Sprint(i), now.Add(16*time.Minute))
	}
	if len(limiter.peers) > 4096 {
		t.Fatal("unbounded peer storage")
	}
}

func TestUnknownAuthRoutesRequireAuthentication(t *testing.T) {
	s := &Server{}
	called := false
	handler := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/future-sensitive-route", nil))
	if w.Code != 401 || called {
		t.Fatalf("unknown auth route exposed: status=%d", w.Code)
	}
}

func TestQueryTokensAreRestrictedToEventStreams(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/api/logs/stream?token=sentinel", true},
		{"/api/exploration/activity/stream?task=1&token=sentinel", true},
		{"/api/side-questions/123/events?token=sentinel", true},
		{"/api/workspace/read?path=private.txt&token=sentinel", false},
		{"/api/auth/change-password?token=sentinel", false},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		if got := extractToken(r); (got != "") != tc.want {
			t.Fatalf("path=%s accepted=%v", tc.path, got != "")
		}
	}
	r := httptest.NewRequest("POST", "/api/logs/stream?token=sentinel", nil)
	if extractToken(r) != "" {
		t.Fatal("query token accepted for mutation")
	}
}
