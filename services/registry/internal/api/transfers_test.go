package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/icofcucam/naditos/packages/go-common/testkit"
)

// seedSellerWithVehicle: returns (seller token, seller user id, vehicle id, plate)
// already wired up so the seller is the current owner. Uses the
// admin pool to bypass the "owners:self" path; what we're testing
// here is the transfer flow, not the claim flow.
func seedSellerWithVehicle(t *testing.T, env *testkit.Env) (string, string, uuid.UUID, string) {
	t.Helper()
	tok, sellerID := env.Token("citizen")
	plate := "TFR-" + uuid.NewString()[:6]
	vid := uuid.New()
	env.Exec(`INSERT INTO vehicles (id, tenant_id, plate)
	          VALUES ($1, $2, $3)`, vid, env.Tenant, plate)
	ownerID := uuid.New()
	env.Exec(`INSERT INTO owners (id, tenant_id, user_id, full_name)
	          VALUES ($1, $2, $3::uuid, 'Seller')`,
		ownerID, env.Tenant, sellerID)
	env.Exec(`UPDATE vehicles SET owner_id=$1 WHERE id=$2`, ownerID, vid)
	return tok, sellerID, vid, plate
}

// seedBuyer: returns the buyer's token and pre-created owners row id
// in the same tenant. The buyer must have an owners row for accept to
// succeed; that mirrors the production order (claim → accept).
func seedBuyer(t *testing.T, env *testkit.Env) (string, string) {
	t.Helper()
	tok, buyerID := env.Token("citizen")
	env.Exec(`INSERT INTO owners (tenant_id, user_id, full_name)
	          VALUES ($1, $2::uuid, 'Buyer')`, env.Tenant, buyerID)
	return tok, buyerID
}

func parseTransfer(t *testing.T, body []byte) (id, code string, expires time.Time) {
	t.Helper()
	var out struct {
		ID        string    `json:"id"`
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("parse transfer: %v body=%s", err, string(body))
	}
	return out.ID, out.Code, out.ExpiresAt
}

// TestTransfer_HappyPath: seller starts → buyer accepts →
// vehicle.owner_id flips, transfer.status='accepted'.
func TestTransfer_HappyPath(t *testing.T) {
	env := testkit.Setup(t)
	sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
	buyerTok, buyerID := seedBuyer(t, env)
	h := build(env)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"buyer@example.com"}`, sellerTok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	_, code, _ := parseTransfer(t, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/accept",
		`{"code":"`+code+`"}`, buyerTok))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}

	var newOwner string
	if err := env.QueryRow(
		`SELECT owner_id::text FROM vehicles WHERE id=$1`, vid).
		Scan(&newOwner); err != nil {
		t.Fatal(err)
	}
	// Buyer's owner row id, not the seller's.
	var expected string
	if err := env.QueryRow(
		`SELECT id::text FROM owners WHERE user_id=$1::uuid`, buyerID).
		Scan(&expected); err != nil {
		t.Fatal(err)
	}
	if newOwner != expected {
		t.Fatalf("owner not flipped: got %s want %s", newOwner, expected)
	}

	var status string
	if err := env.QueryRow(
		`SELECT status FROM vehicle_transfers WHERE vehicle_id=$1`, vid).
		Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "accepted" {
		t.Fatalf("transfer status: want accepted, got %s", status)
	}
}

// TestTransfer_NonOwnerForbidden: a citizen who is NOT the current
// owner of the vehicle gets 403 from /transfer — the JWT alone is
// not enough.
func TestTransfer_NonOwnerForbidden(t *testing.T) {
	env := testkit.Setup(t)
	_, _, vid, _ := seedSellerWithVehicle(t, env)
	otherTok, _ := env.Token("citizen") // unrelated citizen

	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"buyer@example.com"}`, otherTok))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner: want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestTransfer_OnePendingAtATime: starting a second transfer for the
// same vehicle while the first is pending must 409.
func TestTransfer_OnePendingAtATime(t *testing.T) {
	env := testkit.Setup(t)
	sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
	h := build(env)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"a@x.com"}`, sellerTok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first start: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"b@x.com"}`, sellerTok))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second start: want 409, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestTransfer_BadCode_NotFound: an unknown / mistyped code returns
// 404, not 400 — we don't tell the caller whether their code is
// "almost right" because that's an oracle.
func TestTransfer_BadCode_NotFound(t *testing.T) {
	env := testkit.Setup(t)
	buyerTok, _ := seedBuyer(t, env)
	rec := httptest.NewRecorder()
	build(env).ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/accept",
		`{"code":"BOGUS1"}`, buyerTok))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown code: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestTransfer_BuyerWithoutOwner_412: a citizen who hasn't created
// their owners row yet cannot accept; we surface a clear 412 with a
// hint to call /v1/citizens/me/owner first.
func TestTransfer_BuyerWithoutOwner_412(t *testing.T) {
	env := testkit.Setup(t)
	sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
	noOwnerTok, _ := env.Token("citizen") // no owners row
	h := build(env)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"x@x.com"}`, sellerTok))
	_, code, _ := parseTransfer(t, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/accept",
		`{"code":"`+code+`"}`, noOwnerTok))
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("no-owner accept: want 412, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestTransfer_CancelByOwnerOnly: the seller can cancel a pending
// transfer; another citizen can't.
func TestTransfer_CancelByOwnerOnly(t *testing.T) {
	env := testkit.Setup(t)
	sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
	otherTok, _ := env.Token("citizen")
	h := build(env)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"x@x.com"}`, sellerTok))
	id, _, _ := parseTransfer(t, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/"+id+"/cancel", "", otherTok))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner cancel: want 404, got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/"+id+"/cancel", "", sellerTok))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("seller cancel: want 204, got %d %s", rec.Code, rec.Body.String())
	}

	var status string
	if err := env.QueryRow(
		`SELECT status FROM vehicle_transfers WHERE id=$1::uuid`, id).
		Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("status: want cancelled, got %s", status)
	}
}

// TestTransfer_CodeHiddenAfterTerminal: GET /v1/citizens/me/transfers
// must redact the code on cancelled / accepted / expired rows so a
// stale UI doesn't leak still-typeable codes.
func TestTransfer_CodeHiddenAfterTerminal(t *testing.T) {
	env := testkit.Setup(t)
	sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
	h := build(env)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"x@x.com"}`, sellerTok))
	id, _, _ := parseTransfer(t, rec.Body.Bytes())
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/"+id+"/cancel", "", sellerTok))

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("GET", "/v1/citizens/me/transfers", "", sellerTok))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			Status string `json:"status"`
			Code   string `json:"code"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("want 1 transfer, got %d", len(list.Items))
	}
	if list.Items[0].Status != "cancelled" {
		t.Fatalf("status: %s", list.Items[0].Status)
	}
	if list.Items[0].Code != "" {
		t.Fatalf("code on cancelled row not redacted: %q", list.Items[0].Code)
	}
}

// TestTransfer_FlaggedVehicle_Blocked: a vehicle marked stolen,
// seized, or wanted cannot be transferred. The seller's start
// returns 409 not_transferable with a generic message — we must
// not leak which flag is set (is_wanted in particular is
// operational).
func TestTransfer_FlaggedVehicle_Blocked(t *testing.T) {
	cases := []string{"is_stolen", "is_seized", "is_wanted"}
	for _, flag := range cases {
		flag := flag
		t.Run(flag, func(t *testing.T) {
			env := testkit.Setup(t)
			sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
			env.Exec(`UPDATE vehicles SET `+flag+`=true WHERE id=$1`, vid)

			rec := httptest.NewRecorder()
			build(env).ServeHTTP(rec, env.Req("POST",
				"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
				`{"to_contact":"x@x.com"}`, sellerTok))
			if rec.Code != http.StatusConflict {
				t.Fatalf("flag=%s start: want 409, got %d %s",
					flag, rec.Code, rec.Body.String())
			}
			// The body must not name the flag (especially is_wanted).
			body := rec.Body.String()
			for _, leaky := range []string{"stolen", "seized", "wanted"} {
				if containsCI(body, leaky) {
					t.Errorf("flag=%s response leaks %q: %s", flag, leaky, body)
				}
			}
		})
	}
}

// TestTransfer_FlaggedDuringWindow_BlocksAccept: a vehicle that
// becomes flagged after the seller generates a code must not
// transfer when the buyer redeems. The pending transfer is
// cancelled so the stale code can't be retried.
func TestTransfer_FlaggedDuringWindow_BlocksAccept(t *testing.T) {
	env := testkit.Setup(t)
	sellerTok, _, vid, _ := seedSellerWithVehicle(t, env)
	buyerTok, _ := seedBuyer(t, env)
	h := build(env)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/vehicles/"+vid.String()+"/transfer",
		`{"to_contact":"x@x.com"}`, sellerTok))
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	_, code, _ := parseTransfer(t, rec.Body.Bytes())

	// Police flags it after the seller already shared the code.
	env.Exec(`UPDATE vehicles SET is_wanted=true WHERE id=$1`, vid)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, env.Req("POST",
		"/v1/citizens/me/transfers/accept",
		`{"code":"`+code+`"}`, buyerTok))
	if rec.Code != http.StatusConflict {
		t.Fatalf("accept after flag: want 409, got %d %s",
			rec.Code, rec.Body.String())
	}
	// Pending row must be marked cancelled so a buyer can't retry.
	var status string
	if err := env.QueryRow(
		`SELECT status FROM vehicle_transfers WHERE vehicle_id=$1`, vid).
		Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("transfer status: want cancelled, got %s", status)
	}
	// Owner must NOT have flipped.
	var owner string
	_ = env.QueryRow(`SELECT COALESCE(owner_id::text,'') FROM vehicles WHERE id=$1`, vid).
		Scan(&owner)
	if owner == "" {
		t.Fatal("owner_id was cleared — should still be the seller's owners row")
	}
}

// containsCI is a tiny case-insensitive contains check, kept local
// so we don't pull in strings just for one assertion.
func containsCI(haystack, needle string) bool {
	h := []byte(haystack)
	n := []byte(needle)
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := 0; j < len(n); j++ {
			a, b := h[i+j], n[j]
			if a >= 'A' && a <= 'Z' { a += 'a' - 'A' }
			if b >= 'A' && b <= 'Z' { b += 'a' - 'A' }
			if a != b { match = false; break }
		}
		if match { return true }
	}
	return false
}
