package publicchannel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Post is a plaintext public-channel message stored on the gateway for feed readers.
type Post struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

type postsFilePayload struct {
	Posts map[string][]Post `json:"posts"`
}

const (
	maxPostsPerChannel = 2000
	maxPostTextLen     = 32000
	defaultPostLimit   = 500
)

// PostStore persists public-channel posts on the gateway node.
type PostStore struct {
	path  string
	mu    sync.RWMutex
	posts map[string][]Post
}

// NewPostStore loads or creates a public-channel post store file.
func NewPostStore(path string) (*PostStore, error) {
	if path == "" {
		path = "data/public-channel-posts.json"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create public channel post store dir: %w", err)
		}
	}
	s := &PostStore{
		path:  path,
		posts: make(map[string][]Post),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PostStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read public channel post store: %w", err)
	}
	var payload postsFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse public channel post store: %w", err)
	}
	if payload.Posts != nil {
		s.posts = payload.Posts
	}
	return nil
}

func (s *PostStore) persistLocked() error {
	payload := postsFilePayload{Posts: s.posts}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newPostID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Append stores a post for a registered channel address.
func (s *PostStore) Append(channelAddress, from, text string, timestamp int64) (Post, error) {
	ch := normalizeAddress(channelAddress)
	fromAddr := normalizeAddress(from)
	text = strings.TrimSpace(text)
	if ch == "" || fromAddr == "" {
		return Post{}, fmt.Errorf("channel address and from are required")
	}
	if text == "" {
		return Post{}, fmt.Errorf("text is required")
	}
	if len(text) > maxPostTextLen {
		return Post{}, fmt.Errorf("text too long")
	}
	if !strings.HasPrefix(strings.ToLower(ch), "px") || !strings.HasPrefix(strings.ToLower(fromAddr), "px") {
		return Post{}, fmt.Errorf("channel and from must be Platarium wallets")
	}
	if timestamp <= 0 {
		return Post{}, fmt.Errorf("timestamp must be positive")
	}

	id, err := newPostID()
	if err != nil {
		return Post{}, err
	}
	post := Post{
		ID:        id,
		From:      fromAddr,
		Text:      text,
		Timestamp: timestamp,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.posts[ch]
	list = append(list, post)
	if len(list) > maxPostsPerChannel {
		list = list[len(list)-maxPostsPerChannel:]
	}
	s.posts[ch] = list
	if err := s.persistLocked(); err != nil {
		return Post{}, err
	}
	return post, nil
}

// List returns posts for a channel with timestamp > since, oldest first.
func (s *PostStore) List(channelAddress string, since int64, limit int) []Post {
	ch := normalizeAddress(channelAddress)
	if ch == "" {
		return nil
	}
	if limit <= 0 {
		limit = defaultPostLimit
	}
	if limit > maxPostsPerChannel {
		limit = maxPostsPerChannel
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	src := s.posts[ch]
	out := make([]Post, 0, len(src))
	for _, p := range src {
		if p.Timestamp > since {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp == out[j].Timestamp {
			return out[i].ID < out[j].ID
		}
		return out[i].Timestamp < out[j].Timestamp
	})
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
