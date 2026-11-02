package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFreshDBAtLatestVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.version()
	if err != nil || v != len(migrations) {
		t.Fatalf("version = %d, %v", v, err)
	}
	s.Close()
	s, err = Open(path) // reopen is a no-op
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if entries, _ := os.ReadDir(filepath.Join(filepath.Dir(path), "backups")); len(entries) != 0 {
		t.Fatalf("no backup expected for a fresh DB, got %d", len(entries))
	}
}

func TestBackupBeforeMigratingPopulatedDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	withV2 := append(append([]migration{}, migrations...), migration{name: "0002_test.sql", sql: "CREATE TABLE extra(x INTEGER);"})
	for i := 0; i < 6; i++ {
		// Reset to v1 each round so every open applies v2 and backs up first.
		s, err = openWith(path, migrations)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.w.Exec("DROP TABLE IF EXISTS extra; PRAGMA user_version = 1"); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = openWith(path, withV2)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		time.Sleep(2 * time.Millisecond)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxBackups {
		t.Fatalf("backups = %d, want %d", len(entries), maxBackups)
	}
}

func seedAccount(t *testing.T, s *Store, platform, external string) (Credential, Account) {
	t.Helper()
	cred, accs, err := s.UpsertLogin(Credential{Platform: platform, Subject: "sub-" + external, SealedToken: []byte("t"), Refreshable: true},
		[]Account{{Platform: platform, ExternalID: external, DisplayName: "Name " + external}})
	if err != nil {
		t.Fatal(err)
	}
	return cred, accs[0]
}

func TestUpsertLoginKeepsIDsStable(t *testing.T) {
	s := openTest(t)
	cred1, acc1 := seedAccount(t, s, "youtube", "ch1")
	cred2, accs, err := s.UpsertLogin(Credential{Platform: "youtube", Subject: "sub-ch1", SealedToken: []byte("t2")},
		[]Account{{Platform: "youtube", ExternalID: "ch1", DisplayName: "Renamed"}})
	if err != nil {
		t.Fatal(err)
	}
	if cred2.ID != cred1.ID || accs[0].ID != acc1.ID || accs[0].DisplayName != "Renamed" {
		t.Fatalf("ids changed: cred %s→%s acc %s→%s", cred1.ID, cred2.ID, acc1.ID, accs[0].ID)
	}
	if cred2.Version <= cred1.Version {
		t.Fatalf("version not bumped: %d → %d", cred1.Version, cred2.Version)
	}
}

func TestUpdateCredentialTokenCAS(t *testing.T) {
	s := openTest(t)
	cred, _ := seedAccount(t, s, "tiktok", "u1")
	ok, err := s.UpdateCredentialToken(cred.ID, cred.Version, []byte("new"), time.Now().Add(time.Hour), time.Time{})
	if err != nil || !ok {
		t.Fatalf("first update: %v %v", ok, err)
	}
	ok, err = s.UpdateCredentialToken(cred.ID, cred.Version, []byte("stale"), time.Now(), time.Time{})
	if err != nil || ok {
		t.Fatalf("stale update applied: %v %v", ok, err)
	}
	got, _ := s.GetCredential(cred.ID)
	if string(got.SealedToken) != "new" {
		t.Fatalf("token = %q", got.SealedToken)
	}
}

func newUpload(t *testing.T, s *Store, acc Account) Upload {
	t.Helper()
	u, err := s.InsertUpload(Upload{AccountID: acc.ID, Platform: acc.Platform, FilePath: "/v.mp4", FileSize: 10, Container: "mp4", Status: StatusQueued, Phase: PhasePending})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTransitionUploadRefusesStaleFrom(t *testing.T) {
	s := openTest(t)
	_, acc := seedAccount(t, s, "youtube", "ch1")
	u := newUpload(t, s, acc)
	ok, err := s.TransitionUpload(u.ID, StatusUploading, StatusSucceeded, UploadFields{})
	if err != nil || ok {
		t.Fatalf("transition from wrong status applied: %v %v", ok, err)
	}
	note := "done"
	ok, err = s.TransitionUpload(u.ID, StatusQueued, StatusCancelled, UploadFields{Note: &note})
	if err != nil || !ok {
		t.Fatalf("valid transition failed: %v %v", ok, err)
	}
	got, _ := s.GetUpload(u.ID)
	if got.Status != StatusCancelled || got.Note != "done" {
		t.Fatalf("got %+v", got)
	}
}

func TestClaimNextUploadIsExclusive(t *testing.T) {
	s := openTest(t)
	var accs []Account
	for _, id := range []string{"a", "b", "c", "d"} {
		_, acc := seedAccount(t, s, "youtube", id)
		accs = append(accs, acc)
	}
	want := map[string]bool{}
	for i := 0; i < 20; i++ {
		want[newUpload(t, s, accs[i%len(accs)]).ID] = true
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				u, err := s.ClaimNextUpload(time.Now(), nil)
				if err != nil {
					t.Error(err)
					return
				}
				if u == nil {
					return
				}
				mu.Lock()
				seen[u.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != len(want) {
		t.Fatalf("claimed %d of %d", len(seen), len(want))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("job %s claimed %d times", id, n)
		}
	}
}

func TestClaimHonorsNotBeforeAndSerialization(t *testing.T) {
	s := openTest(t)
	_, acc := seedAccount(t, s, "tiktok", "u1")
	first := newUpload(t, s, acc)
	newUpload(t, s, acc)
	u, err := s.ClaimNextUpload(time.Now(), []string{"tiktok"})
	if err != nil || u == nil || u.ID != first.ID {
		t.Fatalf("first claim = %v, %v", u, err)
	}
	if u, _ := s.ClaimNextUpload(time.Now(), []string{"tiktok"}); u != nil {
		t.Fatal("second job for the same serialized account must wait")
	}
	if u, _ := s.ClaimNextUpload(time.Now(), nil); u == nil {
		t.Fatal("without serialization the second job is claimable")
	}

	_, acc2 := seedAccount(t, s, "youtube", "ch9")
	later := newUpload(t, s, acc2)
	nb := time.Now().Add(time.Hour)
	if ok, err := s.TransitionUpload(later.ID, StatusQueued, StatusQueued, UploadFields{NotBefore: &nb}); err != nil || !ok {
		t.Fatalf("set not_before: %v %v", ok, err)
	}
	if u, _ := s.ClaimNextUpload(time.Now(), nil); u != nil {
		t.Fatalf("job with future not_before claimed: %s", u.ID)
	}
	if u, _ := s.ClaimNextUpload(time.Now().Add(2*time.Hour), nil); u == nil {
		t.Fatal("job should be claimable after not_before")
	}
}

func TestResetInterruptedOnlyTouchesUploading(t *testing.T) {
	s := openTest(t)
	_, acc := seedAccount(t, s, "youtube", "ch1")
	up := newUpload(t, s, acc)
	done := newUpload(t, s, acc)
	s.TransitionUpload(up.ID, StatusQueued, StatusUploading, UploadFields{})
	s.TransitionUpload(done.ID, StatusQueued, StatusSucceeded, UploadFields{})
	n, err := s.ResetInterrupted()
	if err != nil || n != 1 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	a, _ := s.GetUpload(up.ID)
	b, _ := s.GetUpload(done.ID)
	if a.Status != StatusQueued || b.Status != StatusSucceeded {
		t.Fatalf("statuses %s %s", a.Status, b.Status)
	}
	active, _ := s.HasActiveUploads(acc.ID)
	if !active {
		t.Fatal("queued job counts as active")
	}
}

func TestReloginDropsOrphanedCredential(t *testing.T) {
	s := openTest(t)
	old, _ := seedAccount(t, s, "youtube", "ch1")
	// Same channel signed in under a new subject: the account moves, the old credential goes.
	if _, _, err := s.UpsertLogin(Credential{Platform: "youtube", Subject: "new-subject", SealedToken: []byte("t")},
		[]Account{{Platform: "youtube", ExternalID: "ch1", DisplayName: "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCredential(old.ID); err != ErrNotFound {
		t.Fatalf("orphaned credential kept: %v", err)
	}
}

func TestMarkStatusIgnoresStaleVersion(t *testing.T) {
	s := openTest(t)
	cred, acc := seedAccount(t, s, "tiktok", "u1")
	newer, _, _ := s.UpsertLogin(Credential{Platform: "tiktok", Subject: cred.Subject, SealedToken: []byte("t2")},
		[]Account{{Platform: "tiktok", ExternalID: "u1", DisplayName: "x"}})
	if err := s.MarkCredentialStatus(cred.ID, cred.Version, StatusNeedsReauth); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCredential(cred.ID); got.Status != StatusOK || got.Version != newer.Version {
		t.Fatalf("stale mark applied: %+v", got)
	}
	if err := s.MarkAccountStatus(acc.ID, acc.UpdatedAt.Add(-time.Second), StatusNeedsReauth); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAccount(acc.ID); got.Status != StatusOK {
		t.Fatalf("stale account mark applied: %s", got.Status)
	}
}
