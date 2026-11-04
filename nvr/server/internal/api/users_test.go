package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func TestUserManagementAndRoles(t *testing.T) {
	e := newEnv(t)
	e.login(t)

	if r := e.do(t, "POST", "/api/v1/users", `{"username":"Ibu","password":"short","role":"viewer"}`, true); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("weak password: %d", r.StatusCode)
	}
	r := e.do(t, "POST", "/api/v1/users", `{"username":"Ibu","password":"panjang-sekali","role":"viewer"}`, true)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}
	var users []userDTO
	json.NewDecoder(r.Body).Decode(&users)
	var viewerID, adminID int64
	for _, u := range users {
		if u.Username == "ibu" {
			viewerID = u.ID
		} else {
			adminID = u.ID
		}
	}
	if viewerID == 0 {
		t.Fatalf("username not normalised: %+v", users)
	}
	if r := e.do(t, "POST", "/api/v1/users", `{"username":"ibu","password":"panjang-sekali","role":"viewer"}`, true); r.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate: %d", r.StatusCode)
	}

	// The viewer can watch but not administer.
	jar, _ := cookiejar.New(nil)
	viewer := &env{srv: e.srv, client: &http.Client{Jar: jar}}
	if r := viewer.do(t, "POST", "/api/v1/auth/login", `{"username":"ibu","password":"panjang-sekali"}`, true); r.StatusCode != 200 {
		t.Fatalf("viewer login: %d", r.StatusCode)
	}
	if r := viewer.do(t, "GET", "/api/v1/cameras", "", true); r.StatusCode != 200 {
		t.Fatalf("viewer cameras: %d", r.StatusCode)
	}
	for _, p := range []string{"/api/v1/users", "/api/v1/settings"} {
		if r := viewer.do(t, "GET", p, "", true); r.StatusCode != http.StatusForbidden {
			t.Fatalf("viewer %s: %d", p, r.StatusCode)
		}
	}
	var devs []deviceDTO
	json.NewDecoder(viewer.do(t, "GET", "/api/v1/auth/sessions", "", true).Body).Decode(&devs)
	if len(devs) != 1 || !devs[0].Current {
		t.Fatalf("viewer sessions = %+v", devs)
	}

	// Guards on the admin's own account and the last admin.
	self := fmt.Sprintf("/api/v1/users/%d", adminID)
	if r := e.do(t, "DELETE", self, "", true); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete self: %d", r.StatusCode)
	}
	if r := e.do(t, "PATCH", self, `{"role":"viewer"}`, true); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("demote self: %d", r.StatusCode)
	}

	// A password reset signs the viewer out everywhere.
	other := fmt.Sprintf("/api/v1/users/%d", viewerID)
	if r := e.do(t, "PATCH", other, `{"password":"password-baru-123"}`, true); r.StatusCode != 200 {
		t.Fatalf("reset: %d", r.StatusCode)
	}
	if r := viewer.do(t, "GET", "/api/v1/auth/me", "", true); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("viewer still signed in after reset: %d", r.StatusCode)
	}
	if r := e.do(t, "PATCH", other, `{"role":"admin"}`, true); r.StatusCode != 200 {
		t.Fatalf("promote: %d", r.StatusCode)
	}
	if r := e.do(t, "DELETE", other, "", true); r.StatusCode != 200 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if r := e.do(t, "PATCH", self, `{"role":"viewer"}`, true); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("last admin demoted: %d", r.StatusCode)
	}
}
