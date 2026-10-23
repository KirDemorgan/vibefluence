package drafts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newStore(t *testing.T) (*Store, *clock, string) {
	t.Helper()
	directory := t.TempDir()
	c := &clock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	store, err := NewStore(directory, c.Now)
	if err != nil {
		t.Fatal(err)
	}
	return store, c, directory
}

func sample(path string) Draft {
	return Draft{Repo: "platform", Path: path, Content: "---\ntitle: Plan\n---\n# Plan\n", Author: "Alex"}
}

func TestSaveGetRoundTrip(t *testing.T) {
	store, _, _ := newStore(t)
	saved, err := store.Save(sample("docs/plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != ID("platform", "docs/plan.md") || len(saved.ID) != 16 {
		t.Errorf("id = %q", saved.ID)
	}
	if saved.Title != "Plan" || saved.Author != "Alex" || !saved.IsNew() {
		t.Errorf("saved = %+v", saved)
	}

	loaded, err := store.Get("platform", saved.ID)
	if err != nil || loaded != saved {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
}

func TestIDIsStableAndDistinct(t *testing.T) {
	first, second := ID("a", "docs/x.md"), ID("a", "docs/x.md")
	if first != second {
		t.Error("the id must be deterministic")
	}
	if ID("a", "docs/x.md") == ID("b", "docs/x.md") || ID("a", "docs/x.md") == ID("a", "docs/y.md") {
		t.Error("different repositories or paths must not collide")
	}
	if ID("a", "bc") == ID("ab", "c") {
		t.Error("the separator must prevent ambiguity")
	}
}

func TestUpdatePreservesCreationTime(t *testing.T) {
	store, clk, _ := newStore(t)
	first, _ := store.Save(sample("docs/plan.md"))
	clk.Advance(time.Hour)

	updated := sample("docs/plan.md")
	updated.Content = "# Changed"
	updated.BaseBlobID = "abc"
	second, err := store.Save(updated)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.Equal(first.CreatedAt.Add(time.Hour)) {
		t.Errorf("created %v updated %v", second.CreatedAt, second.UpdatedAt)
	}
	if second.IsNew() || second.Title != "Changed" {
		t.Errorf("second = %+v", second)
	}
	all, _ := store.List("platform")
	if len(all) != 1 {
		t.Errorf("one draft per path, got %d", len(all))
	}
}

func TestListIsNewestFirstAndScopedToTheRepository(t *testing.T) {
	store, clk, _ := newStore(t)
	for _, path := range []string{"docs/a.md", "docs/b.md", "docs/c.md"} {
		if _, err := store.Save(sample(path)); err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Minute)
	}
	other := sample("docs/other.md")
	other.Repo = "web"
	if _, err := store.Save(other); err != nil {
		t.Fatal(err)
	}

	drafts, err := store.List("platform")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, draft := range drafts {
		paths = append(paths, draft.Path)
	}
	if strings.Join(paths, ",") != "docs/c.md,docs/b.md,docs/a.md" {
		t.Errorf("paths = %v", paths)
	}
}

func TestListOrdersTiesByPath(t *testing.T) {
	store, _, _ := newStore(t)
	_, _ = store.Save(sample("docs/b.md"))
	_, _ = store.Save(sample("docs/a.md"))
	drafts, _ := store.List("platform")
	if drafts[0].Path != "docs/a.md" {
		t.Errorf("ties must be ordered by path: %v", drafts[0].Path)
	}
}

func TestListOfAnUnknownRepositoryIsEmpty(t *testing.T) {
	store, _, _ := newStore(t)
	drafts, err := store.List("never-used")
	if err != nil || drafts == nil || len(drafts) != 0 {
		t.Errorf("drafts = %v, err = %v", drafts, err)
	}
}

func TestDelete(t *testing.T) {
	store, _, _ := newStore(t)
	saved, _ := store.Save(sample("docs/plan.md"))
	if err := store.Delete("platform", saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("platform", saved.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v", err)
	}
	if err := store.Delete("platform", saved.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

func TestGetUnknown(t *testing.T) {
	store, _, _ := newStore(t)
	if _, err := store.Get("platform", "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestIdentifiersCannotEscapeTheDirectory(t *testing.T) {
	store, _, directory := newStore(t)
	secret := filepath.Join(filepath.Dir(directory), "secret.json")
	_ = os.WriteFile(secret, []byte(`{"content":"secret"}`), 0o600)

	attempts := []struct{ repo, id string }{
		{"../..", "0123456789abcdef"},
		{"platform", "../../secret"},
		{"platform", "../../../secret.json"},
		{"platform/x", "0123456789abcdef"},
		{"", "0123456789abcdef"},
		{"Platform", "0123456789abcdef"},
		{"platform", ""},
		{"platform", "0123456789ABCDEF"},
		{"platform", "0123456789abcde"},
		{"platform", "0123456789abcdefg"},
	}
	for _, attempt := range attempts {
		if _, err := store.Get(attempt.repo, attempt.id); !errors.Is(err, ErrInvalid) {
			t.Errorf("Get(%q, %q) = %v", attempt.repo, attempt.id, err)
		}
		if err := store.Delete(attempt.repo, attempt.id); !errors.Is(err, ErrInvalid) {
			t.Errorf("Delete(%q, %q) = %v", attempt.repo, attempt.id, err)
		}
	}
	if _, err := store.List("../.."); !errors.Is(err, ErrInvalid) {
		t.Errorf("List traversal: %v", err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Errorf("the secret file must be untouched: %v", err)
	}
}

func TestSaveValidation(t *testing.T) {
	store, _, _ := newStore(t)
	tests := map[string]func(*Draft){
		"bad repository":    func(d *Draft) { d.Repo = "../etc" },
		"empty repository":  func(d *Draft) { d.Repo = "" },
		"empty path":        func(d *Draft) { d.Path = "" },
		"long path":         func(d *Draft) { d.Path = strings.Repeat("a", maxPathBytes+1) },
		"invalid utf8 path": func(d *Draft) { d.Path = "docs/\xff.md" },
		"huge content":      func(d *Draft) { d.Content = strings.Repeat("x", MaxContentBytes+1) },
		"invalid utf8":      func(d *Draft) { d.Content = "bad \xff bytes" },
		"nul byte":          func(d *Draft) { d.Content = "a\x00b" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			draft := sample("docs/plan.md")
			mutate(&draft)
			if _, err := store.Save(draft); !errors.Is(err, ErrInvalid) {
				t.Errorf("err = %v", err)
			}
		})
	}
	if drafts, _ := store.List("platform"); len(drafts) != 0 {
		t.Errorf("rejected drafts must not be stored: %d", len(drafts))
	}
}

func TestContentAtTheLimitIsAccepted(t *testing.T) {
	store, _, _ := newStore(t)
	draft := sample("docs/big.md")
	draft.Content = strings.Repeat("x", MaxContentBytes)
	if _, err := store.Save(draft); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestAuthorIsSanitised(t *testing.T) {
	store, _, _ := newStore(t)
	draft := sample("docs/plan.md")
	draft.Author = "  Alex\nInjected: header\t\x00  "
	saved, _ := store.Save(draft)
	if strings.ContainsAny(saved.Author, "\n\t\x00") || saved.Author != "AlexInjected: header" {
		t.Errorf("author = %q", saved.Author)
	}

	if got := CleanAuthor(strings.Repeat("я", 200)); len([]rune(got)) != maxAuthorRunes {
		t.Errorf("author length = %d", len([]rune(got)))
	}
	if got := CleanAuthor("   "); got != "" {
		t.Errorf("blank author = %q", got)
	}
}

func TestFilesAreStoredPrivately(t *testing.T) {
	store, _, directory := newStore(t)
	saved, _ := store.Save(sample("docs/plan.md"))

	repoInfo, err := os.Stat(filepath.Join(directory, "drafts", "platform"))
	if err != nil || repoInfo.Mode().Perm() != 0o700 {
		t.Errorf("repository directory: %v %v", repoInfo, err)
	}
	fileInfo, err := os.Stat(filepath.Join(directory, "drafts", "platform", saved.ID+".json"))
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("draft file: %v %v", fileInfo, err)
	}
}

func TestNoTemporaryFilesRemainAfterSaving(t *testing.T) {
	store, _, directory := newStore(t)
	for i := 0; i < 5; i++ {
		_, _ = store.Save(sample("docs/plan.md"))
	}
	entries, _ := os.ReadDir(filepath.Join(directory, "drafts", "platform"))
	if len(entries) != 1 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("entries = %v", names)
	}
}

func TestCorruptedFilesAreSkippedWhenListingAndReportedWhenRead(t *testing.T) {
	store, _, directory := newStore(t)
	good, _ := store.Save(sample("docs/good.md"))
	badID := ID("platform", "docs/bad.md")
	_ = os.WriteFile(filepath.Join(directory, "drafts", "platform", badID+".json"), []byte("{broken"), 0o600)
	_ = os.WriteFile(filepath.Join(directory, "drafts", "platform", "notes.txt"), []byte("ignored"), 0o600)
	_ = os.WriteFile(filepath.Join(directory, "drafts", "platform", "NOTANID.json"), []byte("{}"), 0o600)
	_ = os.Mkdir(filepath.Join(directory, "drafts", "platform", "0123456789abcdef.json"), 0o700)

	drafts, err := store.List("platform")
	if err != nil || len(drafts) != 1 || drafts[0].ID != good.ID {
		t.Fatalf("drafts = %+v, err = %v", drafts, err)
	}
	if _, err := store.Get("platform", badID); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a corrupted draft must be reported, got %v", err)
	}
	if _, err := store.Save(Draft{Repo: "platform", Path: "docs/bad.md", Content: "x"}); err == nil {
		t.Error("overwriting a corrupted draft should surface the corruption")
	}
}

func TestDraftLimitPerRepository(t *testing.T) {
	store, _, directory := newStore(t)
	repoDir := filepath.Join(directory, "drafts", "platform")
	_ = os.MkdirAll(repoDir, 0o700)
	for i := 0; i < MaxDraftsPerRepo; i++ {
		_ = os.WriteFile(filepath.Join(repoDir, fmt.Sprintf("%016x.json", i)), []byte(`{"id":"x","repo":"platform"}`), 0o600)
	}
	if _, err := store.Save(sample("docs/new.md")); !errors.Is(err, ErrLimit) {
		t.Errorf("err = %v", err)
	}
	other := sample("docs/new.md")
	other.Repo = "web"
	if _, err := store.Save(other); err != nil {
		t.Errorf("another repository is unaffected: %v", err)
	}
}

func TestUpdatingAnExistingDraftIsAllowedAtTheLimit(t *testing.T) {
	store, _, directory := newStore(t)
	existing, _ := store.Save(sample("docs/plan.md"))
	repoDir := filepath.Join(directory, "drafts", "platform")
	for i := 0; i < MaxDraftsPerRepo-1; i++ {
		_ = os.WriteFile(filepath.Join(repoDir, fmt.Sprintf("%016x.json", i)), []byte(`{}`), 0o600)
	}
	update := sample("docs/plan.md")
	update.Content = "changed"
	saved, err := store.Save(update)
	if err != nil || saved.ID != existing.ID {
		t.Errorf("err = %v", err)
	}
}

func TestConcurrentSavesDoNotCorruptTheDraft(t *testing.T) {
	store, _, _ := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			draft := sample("docs/shared.md")
			draft.Content = fmt.Sprintf("# Version %d\n%s", i, strings.Repeat("x", 4096))
			if _, err := store.Save(draft); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	loaded, err := store.Get("platform", ID("platform", "docs/shared.md"))
	if err != nil || !strings.HasPrefix(loaded.Content, "# Version ") {
		t.Errorf("loaded = %q, err = %v", loaded.Content, err)
	}
}

func TestNewStoreFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, []byte("x"), 0o600)
	if _, err := NewStore(blocker, nil); err == nil {
		t.Error("expected an error")
	}
}

func TestNewStoreDefaultsTheClock(t *testing.T) {
	store, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := store.Save(sample("docs/plan.md"))
	if time.Since(saved.UpdatedAt) > time.Minute {
		t.Errorf("updated = %v", saved.UpdatedAt)
	}
}

func TestSaveAndDeleteReportDiskFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	store, _, directory := newStore(t)
	saved, err := store.Save(sample("docs/plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(directory, "drafts", "platform")
	if err := os.Chmod(repoDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(repoDir, 0o700) }()

	changed := sample("docs/plan.md")
	changed.Content = "changed"
	if _, err := store.Save(changed); err == nil || !strings.Contains(err.Error(), "temporary draft file") {
		t.Errorf("Save err = %v", err)
	}
	if err := store.Delete("platform", saved.ID); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Delete err = %v", err)
	}
}

func TestSavingIntoAFileInsteadOfADirectoryFails(t *testing.T) {
	store, _, directory := newStore(t)
	blocker := filepath.Join(directory, "drafts", "platform")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(sample("docs/plan.md")); err == nil {
		t.Error("expected an error")
	}
	if _, err := store.List("platform"); err == nil {
		t.Error("listing a non-directory must fail")
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := invalid("bad %s", "input")
	if err.Error() != "bad input" || !errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestSaveKeepsTheMergeRequestOfAnExistingDraft(t *testing.T) {
	store, _, _ := newStore(t)
	saved, err := store.Save(sample("docs/plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMergeRequest("platform", saved.ID, "https://gl/mr/4", 4); err != nil {
		t.Fatal(err)
	}
	edited := sample("docs/plan.md")
	edited.Content = "# Plan\n\nmore"
	if _, err := store.Save(edited); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get("platform", saved.ID)
	if err != nil || loaded.MergeRequestIID != 4 || loaded.MergeRequestURL != "https://gl/mr/4" || loaded.Content != edited.Content {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
}

func TestSetMergeRequestReportsMissingDrafts(t *testing.T) {
	store, _, _ := newStore(t)
	if err := store.SetMergeRequest("platform", ID("platform", "docs/none.md"), "u", 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}
