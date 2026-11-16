package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxAvatarBytes      = 1 << 20
	maxAvatarDownloads  = 4
	avatarFetchTimeout  = 15 * time.Second
	avatarCacheSubdir   = "roundup/avatars"
	avatarMaxRedirects  = 5
	avatarDirMode       = 0o700
	avatarFileMode      = 0o600
	avatarContentPrefix = "image/"
)

// avatarStore hands out downloaded profile photos without ever blocking on the network.
type avatarStore struct {
	client   *http.Client
	dir      string
	onChange func()
	slots    chan struct{}

	mu       sync.Mutex
	have     map[string][]byte
	failed   map[string]bool
	inflight map[string]bool
}

// newAvatarStore caches under dir, which may be empty to skip the disk, and calls onChange after every download ends.
func newAvatarStore(client *http.Client, dir string, onChange func()) *avatarStore {
	return &avatarStore{
		client:   client,
		dir:      dir,
		onChange: onChange,
		slots:    make(chan struct{}, maxAvatarDownloads),
		have:     map[string][]byte{},
		failed:   map[string]bool{},
		inflight: map[string]bool{},
	}
}

func defaultAvatarClient() *http.Client {
	return &http.Client{
		Timeout: avatarFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= avatarMaxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirect to a non-https URL")
			}
			return nil
		},
	}
}

func defaultAvatarDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.FromSlash(avatarCacheSubdir)), nil
}

// Get returns the photo's bytes if it has them; otherwise it starts one background download, unless the URL already failed.
func (s *avatarStore) Get(rawURL string) []byte {
	if s == nil || rawURL == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.have[rawURL]; ok {
		return data
	}
	if s.failed[rawURL] || s.inflight[rawURL] {
		return nil
	}
	if !isHTTPS(rawURL) {
		s.failed[rawURL] = true
		return nil
	}
	if data := s.readCached(rawURL); data != nil {
		s.have[rawURL] = data
		return data
	}
	s.inflight[rawURL] = true
	go s.download(rawURL)
	return nil
}

func (s *avatarStore) download(rawURL string) {
	s.slots <- struct{}{}
	data, err := s.fetch(rawURL)
	<-s.slots
	if err == nil {
		_ = s.save(rawURL, data)
	}

	s.mu.Lock()
	delete(s.inflight, rawURL)
	if data != nil {
		s.have[rawURL] = data
	} else {
		s.failed[rawURL] = true
	}
	s.mu.Unlock()
	if s.onChange != nil {
		s.onChange()
	}
}

func (s *avatarStore) fetch(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading avatar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading avatar: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading avatar: %w", err)
	}
	if len(data) > maxAvatarBytes {
		return nil, errors.New("avatar is over 1 MB")
	}
	if !strings.HasPrefix(http.DetectContentType(data), avatarContentPrefix) {
		return nil, errors.New("avatar is not an image")
	}
	return data, nil
}

// save is best effort: a photo that cannot be cached is still shown this session.
func (s *avatarStore) save(rawURL string, data []byte) error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, avatarDirMode); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, "download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(avatarFileMode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path(rawURL))
}

func (s *avatarStore) readCached(rawURL string) []byte {
	if s.dir == "" {
		return nil
	}
	data, err := os.ReadFile(s.path(rawURL))
	if err != nil {
		return nil
	}
	return data
}

func (s *avatarStore) path(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:]))
}

func isHTTPS(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
