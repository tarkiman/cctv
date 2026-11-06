package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/tarkiman/cctv/nvr/server/internal/store"
)

// A viewer limited to one location must not reach the other location's camera through
// any endpoint; it gets 404 as if the camera did not exist.
func TestViewerLocationAccess(t *testing.T) {
	e := newEnv(t) // camera "bardi" in the default location
	e.login(t)
	ctx := context.Background()

	r := e.do(t, "POST", "/api/v1/locations", `{"name":"Rumah Ibu"}`, true)
	var locs []locationDTO
	json.NewDecoder(r.Body).Decode(&locs)
	if len(locs) != 2 {
		t.Fatalf("locations = %+v", locs)
	}
	ibu := locs[1].ID
	other, _ := e.store.Cameras.CreateDiscovered(ctx, "garasi", "garasi", sql.NullString{}, 1)
	if r := e.do(t, "PATCH", fmt.Sprintf("/api/v1/cameras/%d", other), fmt.Sprintf(`{"location_id":%d}`, ibu), true); r.StatusCode != 200 {
		t.Fatalf("move camera: %d", r.StatusCode)
	}
	rec := &store.Recording{CameraID: e.camID, StartAt: 1000, EndAt: 1600, Path: "x.mp4", SizeBytes: 1}
	e.store.Recordings.Insert(ctx, rec)

	e.do(t, "POST", "/api/v1/users", `{"username":"ibu","password":"panjang-sekali","role":"viewer"}`, true)
	var users []userDTO
	json.NewDecoder(e.do(t, "GET", "/api/v1/users", "", true).Body).Decode(&users)
	var ibuID int64
	for _, u := range users {
		if u.Username == "ibu" {
			ibuID = u.ID
		}
	}
	grant := fmt.Sprintf(`{"access_all":false,"locations":[%d],"cameras":[],"deny_cameras":[]}`, ibu)
	if r := e.do(t, "PUT", fmt.Sprintf("/api/v1/users/%d/access", ibuID), grant, true); r.StatusCode != 200 {
		t.Fatalf("set access: %d", r.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	v := &env{srv: e.srv, client: &http.Client{Jar: jar}}
	v.do(t, "POST", "/api/v1/auth/login", `{"username":"ibu","password":"panjang-sekali"}`, true)

	var cams []cameraDTO
	json.NewDecoder(v.do(t, "GET", "/api/v1/cameras", "", true).Body).Decode(&cams)
	if len(cams) != 1 || cams[0].ID != other {
		t.Fatalf("viewer cameras = %+v", cams)
	}
	json.NewDecoder(v.do(t, "GET", "/api/v1/locations", "", true).Body).Decode(&locs)
	if len(locs) != 1 || locs[0].ID != ibu {
		t.Fatalf("viewer locations = %+v", locs)
	}
	hidden := e.camID
	for _, p := range []string{
		fmt.Sprintf("/api/v1/cameras/%d", hidden),
		fmt.Sprintf("/api/v1/cameras/%d/snapshot.jpg", hidden),
		fmt.Sprintf("/api/v1/cameras/%d/timeline?date=2026-11-06", hidden),
		fmt.Sprintf("/api/v1/cameras/%d/events?from=2026-11-06T00:00:00Z&to=2026-11-07T00:00:00Z", hidden),
		fmt.Sprintf("/api/v1/cameras/%d/export?from=2026-11-06T00:00:00Z&to=2026-11-06T00:10:00Z", hidden),
		fmt.Sprintf("/api/v1/recordings/%d/file", rec.ID),
		fmt.Sprintf("/api/v1/recordings/%d/next", rec.ID),
		fmt.Sprintf("/api/v1/timelapse/%d/2026-11-05.mp4", hidden),
		fmt.Sprintf("/api/v1/live/ws?camera=%d", hidden),
	} {
		if r := v.do(t, "GET", p, "", true); r.StatusCode != http.StatusNotFound {
			t.Errorf("viewer GET %s = %d, want 404", p, r.StatusCode)
		}
	}
	// Excluding a camera inside a granted location hides it too.
	grant = fmt.Sprintf(`{"access_all":false,"locations":[%d],"cameras":[],"deny_cameras":[%d]}`, ibu, other)
	e.do(t, "PUT", fmt.Sprintf("/api/v1/users/%d/access", ibuID), grant, true)
	if r := v.do(t, "GET", fmt.Sprintf("/api/v1/cameras/%d", other), "", true); r.StatusCode != http.StatusNotFound {
		t.Errorf("denied camera = %d, want 404", r.StatusCode)
	}
	// The admin still sees everything; a non-empty location cannot be deleted.
	json.NewDecoder(e.do(t, "GET", "/api/v1/cameras", "", true).Body).Decode(&cams)
	if len(cams) != 2 {
		t.Fatalf("admin cameras = %d", len(cams))
	}
	if r := e.do(t, "DELETE", fmt.Sprintf("/api/v1/locations/%d", ibu), "", true); r.StatusCode != http.StatusConflict {
		t.Fatalf("delete non-empty location = %d", r.StatusCode)
	}
}
