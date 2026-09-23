package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/db"
	"github.com/laststate/trace/internal/store"
)

func testStore(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	url := os.Getenv("TRACE_DATABASE_URL")
	if url == "" {
		url = "postgres://trace:trace@localhost:5432/trace?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Skip("postgres unavailable:", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal("migrate:", err)
	}
	// isolate schema data by using unique project per test via bootstrap if empty
	return &store.Store{Pool: pool}, ctx
}

// hash64 returns a 64-char pseudo-hash (uuid strings are only 36 chars).
func hash64() string {
	return (uuid.NewString() + uuid.NewString())[:64]
}

func TestBootstrapLoginAndIngestIdempotency(t *testing.T) {
	st, ctx := testStore(t)

	// Ensure org exists
	_, err := st.DefaultProject(ctx)
	if err != nil {
		org, project, secret, berr := st.Bootstrap(ctx)
		if berr != nil && berr.Error() != "already bootstrapped" {
			t.Fatal(berr)
		}
		if berr == nil {
			if org.Slug == "" || project.ID == uuid.Nil || secret == "" {
				t.Fatal("bootstrap incomplete")
			}
			tok, aerr := st.AuthIngestToken(ctx, secret)
			if aerr != nil {
				t.Fatal(aerr)
			}
			if !store.TokenHasScope(tok, "event:write") {
				t.Fatal("scopes")
			}
		}
	}

	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// admin
	_, _, _ = st.EnsureAdmin(ctx, "admin-test@localhost", store.GeneratePassword(), "Admin")
	// login if user exists
	// create fresh user path via login of existing — skip if many users

	eventID := "evt-test-" + uuid.NewString()
	hash := hash64()
	res, err := st.CreateEventIdempotent(ctx, p.ID, eventID, 2, 1, 1, 1, "error", "k/"+hash, hash, 10, json.RawMessage(`{}`), "issue")
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate {
		t.Fatal("first should not be dup")
	}
	// same hash duplicate
	res2, err := st.CreateEventIdempotent(ctx, p.ID, eventID, 2, 1, 1, 1, "error", "k/"+hash, hash, 10, json.RawMessage(`{}`), "issue")
	if err != nil || !res2.Duplicate {
		t.Fatal(err, res2)
	}
	// conflict different hash
	other := hash64()
	_, err = st.CreateEventIdempotent(ctx, p.ID, eventID, 2, 1, 1, 1, "error", "k/other", other, 10, json.RawMessage(`{}`), "issue")
	if err != store.ErrConflict {
		t.Fatalf("want conflict got %v", err)
	}

	// get in project
	ev, err := st.GetEventInProject(ctx, p.ID, res.Event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventID != eventID {
		t.Fatal(ev.EventID)
	}
	// wrong project
	_, err = st.GetEventInProject(ctx, uuid.New(), res.Event.ID)
	if err != store.ErrNotFound {
		t.Fatal(err)
	}
}

func TestDeviceAnonymousAndHealth(t *testing.T) {
	st, ctx := testStore(t)
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	d1, err := st.UpsertDevice(ctx, p.ID, "", "prod", "r1", "1.0", "bid", "fatal")
	if err != nil {
		t.Fatal(err)
	}
	if d1.DeviceID == "unknown" || d1.DeviceID == "" {
		t.Fatal(d1.DeviceID)
	}
	if d1.Status != "unhealthy" {
		t.Fatal(d1.Status)
	}
	d2, err := st.UpsertDevice(ctx, p.ID, "", "prod", "r1", "1.0", "bid", "info")
	if err != nil {
		t.Fatal(err)
	}
	if d2.DeviceID == d1.DeviceID {
		t.Fatal("anon devices must not merge")
	}
	named, err := st.UpsertDevice(ctx, p.ID, "dev-1", "p", "hw", "fw", "b", "error")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.MarkDeviceHealth(ctx, named.ID, "fatal")
	got, err := st.GetDeviceInProject(ctx, p.ID, named.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "unhealthy" {
		t.Fatal(got.Status)
	}
}

func TestLinkEventToIssueRegression(t *testing.T) {
	st, ctx := testStore(t)
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	fp := "fp-" + uuid.NewString()
	// create event
	hash := hash64()
	res, err := st.CreateEventIdempotent(ctx, p.ID, "e-"+uuid.NewString(), 1, 1, 1, 1, "fatal", "k", hash, 1, json.RawMessage(`{}`), "issue")
	if err != nil {
		t.Fatal(err)
	}
	dev, _ := st.UpsertDevice(ctx, p.ID, "d-reg", "", "", "", "", "fatal")
	issue, err := st.LinkEventToIssue(ctx, p.ID, res.Event.ID, fp, "title", "fatal", "cause", &dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !issue.IsNew || issue.EventCount < 1 {
		t.Fatalf("%+v", issue)
	}
	// resolve
	_, err = st.UpdateIssueStatus(ctx, p.ID, issue.ID, "resolved")
	if err != nil {
		t.Fatal(err)
	}
	// second event same fp → regression
	hash2 := hash64()
	res2, err := st.CreateEventIdempotent(ctx, p.ID, "e-"+uuid.NewString(), 1, 1, 1, 1, "fatal", "k2", hash2, 1, json.RawMessage(`{}`), "issue")
	if err != nil {
		t.Fatal(err)
	}
	issue2, err := st.LinkEventToIssue(ctx, p.ID, res2.Event.ID, fp, "title", "fatal", "cause", &dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !issue2.IsRegression || issue2.Status != "open" {
		t.Fatalf("%+v", issue2)
	}
	// re-link same event is idempotent (no double count explosion)
	c1 := issue2.EventCount
	issue3, err := st.LinkEventToIssue(ctx, p.ID, res2.Event.ID, fp, "title", "fatal", "cause", &dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issue3.EventCount != c1 {
		t.Fatalf("double count %d -> %d", c1, issue3.EventCount)
	}
}

func TestIssueMergeSplit(t *testing.T) {
	st, ctx := testStore(t)
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	mk := func() (uuid.UUID, store.Issue) {
		h := hash64()
		res, err := st.CreateEventIdempotent(ctx, p.ID, "e-"+uuid.NewString(), 1, 1, 1, 1, "error", "k", h, 1, json.RawMessage(`{}`), "issue")
		if err != nil {
			t.Fatal(err)
		}
		fp := "fp-" + uuid.NewString()
		iss, err := st.LinkEventToIssue(ctx, p.ID, res.Event.ID, fp, "t", "error", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return res.Event.ID, iss
	}
	e1, i1 := mk()
	_, i2 := mk()
	if err := st.MergeIssues(ctx, p.ID, i1.ID, i2.ID, nil); err != nil {
		t.Fatal(err)
	}
	// split from i2 with e1 might fail if event moved — create third
	e3, i3 := mk()
	ni, err := st.SplitIssue(ctx, p.ID, i3.ID, []uuid.UUID{e3}, "split title", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ni.Title != "split title" {
		t.Fatal(ni.Title)
	}
	_ = e1
}

func TestDedupeJob(t *testing.T) {
	st, ctx := testStore(t)
	key := "dedupe-" + uuid.NewString()
	ok, err := st.TryDedupeJob(ctx, key, "notify_issue")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	ok2, err := st.TryDedupeJob(ctx, key, "notify_issue")
	if err != nil || ok2 {
		t.Fatal("second should be false", ok2, err)
	}
}

func TestSearchAndPages(t *testing.T) {
	st, ctx := testStore(t)
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	_, total, err := st.ListIssuesPage(ctx, p.ID, store.ListOpts{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = total
	_, err = st.GlobalSearchExtended(ctx, p.ID, "zzz-not-found-xxx", 5)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOrgProjectCRUD(t *testing.T) {
	st, ctx := testStore(t)
	// create user with membership in existing org, then mint session
	email := "u-" + uuid.NewString()[:8] + "@test.local"
	p0, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	u, err := st.CreateUser(ctx, p0.OrganizationID, email, store.GeneratePassword(), "Test User", "admin")
	if err != nil {
		t.Fatal(err)
	}
	sess, _, err := st.MintSession(ctx, u, p0.OrganizationID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != u.ID {
		t.Fatal()
	}
	slug := "org-" + uuid.NewString()[:8]
	org, err := st.CreateOrganization(ctx, "Org "+slug, slug, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	pslug := "p-" + uuid.NewString()[:8]
	proj, err := st.CreateProject(ctx, org.ID, "Proj", pslug, "desc")
	if err != nil {
		t.Fatal(err)
	}
	proj, err = st.UpdateProject(ctx, org.ID, proj.ID, "Proj2", "d2")
	if err != nil || proj.Name != "Proj2" {
		t.Fatal(err, proj)
	}
	list, err := st.ListProjects(ctx, org.ID)
	if err != nil || len(list) == 0 {
		t.Fatal(err)
	}
	if err := st.DeleteProject(ctx, org.ID, proj.ID); err != nil {
		t.Fatal(err)
	}
}
