package drafts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"vibefluence/internal/docs"
)

const (
	MaxContentBytes   = 1 << 20
	MaxDraftsPerRepo  = 500
	maxAuthorRunes    = 80
	maxPathBytes      = 240
	draftFileSuffix   = ".json"
	directoryMode     = 0o700
	fileMode          = 0o600
	temporaryFilename = ".tmp-"
)

var (
	ErrNotFound = errors.New("draft not found")
	ErrInvalid  = errors.New("invalid draft")
	ErrLimit    = errors.New("too many drafts")

	repoPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	idPattern   = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

type Draft struct {
	ID         string    `json:"id"`
	Repo       string    `json:"repo"`
	Path       string    `json:"path"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	BaseBlobID string    `json:"baseBlobId"`
	Author     string    `json:"author"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	MergeRequestURL string `json:"mergeRequestUrl,omitempty"`
	MergeRequestIID int    `json:"mergeRequestIid,omitempty"`
}

func (d Draft) IsNew() bool {
	return d.BaseBlobID == ""
}

type validationError struct{ message string }

func (e *validationError) Error() string        { return e.message }
func (e *validationError) Is(target error) bool { return target == ErrInvalid }

func invalid(format string, args ...any) error {
	return &validationError{message: fmt.Sprintf(format, args...)}
}

func ID(repo, filePath string) string {
	sum := sha256.Sum256([]byte(repo + "\x00" + filePath))
	return hex.EncodeToString(sum[:8])
}

type Store struct {
	dir string
	now func() time.Time
	mu  sync.Mutex
}

func NewStore(dir string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	root := filepath.Join(dir, "drafts")
	if err := os.MkdirAll(root, directoryMode); err != nil {
		return nil, fmt.Errorf("create drafts directory: %w", err)
	}
	return &Store{dir: root, now: now}, nil
}

func (s *Store) repoDir(repo string) (string, error) {
	if !repoPattern.MatchString(repo) {
		return "", invalid("repository id %q is not valid", repo)
	}
	return filepath.Join(s.dir, repo), nil
}

func (s *Store) file(repo, id string) (string, error) {
	directory, err := s.repoDir(repo)
	if err != nil {
		return "", err
	}
	if !idPattern.MatchString(id) {
		return "", invalid("draft id %q is not valid", id)
	}
	return filepath.Join(directory, id+draftFileSuffix), nil
}

func Validate(draft Draft) error {
	if !repoPattern.MatchString(draft.Repo) {
		return invalid("repository id %q is not valid", draft.Repo)
	}
	if draft.Path == "" || len(draft.Path) > maxPathBytes || !utf8.ValidString(draft.Path) {
		return invalid("the document path is empty, too long or not valid UTF-8")
	}
	if len(draft.Content) > MaxContentBytes {
		return invalid("the document is larger than %d bytes", MaxContentBytes)
	}
	if !utf8.ValidString(draft.Content) || strings.ContainsRune(draft.Content, 0) {
		return invalid("the document must be valid UTF-8 text")
	}
	return nil
}

func CleanAuthor(author string) string {
	var out strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(author) {
		if unicode.IsControl(r) {
			continue
		}
		if count >= maxAuthorRunes {
			break
		}
		out.WriteRune(r)
		count++
	}
	return strings.TrimSpace(out.String())
}

func (s *Store) Save(draft Draft) (Draft, error) {
	if err := Validate(draft); err != nil {
		return Draft{}, err
	}
	draft.ID = ID(draft.Repo, draft.Path)
	draft.Author = CleanAuthor(draft.Author)
	draft.Title = docs.Parse([]byte(draft.Content), draft.Path).Title

	s.mu.Lock()
	defer s.mu.Unlock()

	target, err := s.file(draft.Repo, draft.ID)
	if err != nil {
		return Draft{}, err
	}
	now := s.now().UTC()
	draft.UpdatedAt = now
	draft.CreatedAt = now
	if existing, err := s.read(target); err == nil {
		draft.CreatedAt = existing.CreatedAt
		if draft.MergeRequestIID == 0 {
			draft.MergeRequestURL = existing.MergeRequestURL
			draft.MergeRequestIID = existing.MergeRequestIID
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Draft{}, err
	} else if count, err := s.count(filepath.Dir(target)); err != nil {
		return Draft{}, err
	} else if count >= MaxDraftsPerRepo {
		return Draft{}, fmt.Errorf("%w: a repository can hold %d drafts", ErrLimit, MaxDraftsPerRepo)
	}

	if err := writeAtomically(target, draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func (s *Store) SetMergeRequest(repo, id, url string, iid int) error {
	target, err := s.file(repo, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, err := s.read(target)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	draft.MergeRequestURL = url
	draft.MergeRequestIID = iid
	return writeAtomically(target, draft)
}

func (s *Store) Get(repo, id string) (Draft, error) {
	target, err := s.file(repo, id)
	if err != nil {
		return Draft{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, err := s.read(target)
	if errors.Is(err, fs.ErrNotExist) {
		return Draft{}, ErrNotFound
	}
	return draft, err
}

func (s *Store) Delete(repo, id string) error {
	target, err := s.file(repo, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete draft: %w", err)
	}
	return nil
}

func (s *Store) List(repo string) ([]Draft, error) {
	directory, err := s.repoDir(repo)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return []Draft{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}
	drafts := make([]Draft, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, draftFileSuffix) || !idPattern.MatchString(strings.TrimSuffix(name, draftFileSuffix)) {
			continue
		}
		draft, err := s.read(filepath.Join(directory, name))
		if err != nil {
			continue
		}
		drafts = append(drafts, draft)
	}
	sort.Slice(drafts, func(i, j int) bool {
		if !drafts[i].UpdatedAt.Equal(drafts[j].UpdatedAt) {
			return drafts[i].UpdatedAt.After(drafts[j].UpdatedAt)
		}
		return drafts[i].Path < drafts[j].Path
	})
	return drafts, nil
}

func (s *Store) read(target string) (Draft, error) {
	raw, err := os.ReadFile(target)
	if err != nil {
		return Draft{}, err
	}
	var draft Draft
	if err := json.Unmarshal(raw, &draft); err != nil {
		return Draft{}, fmt.Errorf("draft file %s is corrupted: %w", filepath.Base(target), err)
	}
	return draft, nil
}

func (s *Store) count(directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("count drafts: %w", err)
	}
	total := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), draftFileSuffix) {
			total++
		}
	}
	return total, nil
}

func writeAtomically(target string, draft Draft) error {
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, directoryMode); err != nil {
		return fmt.Errorf("create repository drafts directory: %w", err)
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		return fmt.Errorf("encode draft: %w", err)
	}
	temporary, err := os.CreateTemp(directory, temporaryFilename+"*")
	if err != nil {
		return fmt.Errorf("create temporary draft file: %w", err)
	}
	cleanup := func() { _ = os.Remove(temporary.Name()) }
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write draft: %w", err)
	}
	if err := temporary.Chmod(fileMode); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("protect draft file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("sync draft: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close draft: %w", err)
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		cleanup()
		return fmt.Errorf("publish draft file: %w", err)
	}
	return nil
}
