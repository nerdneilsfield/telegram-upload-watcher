package queue

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	StatusQueued  = "queued"
	StatusSending = "sending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

var pendingStatuses = map[string]bool{
	StatusQueued: true,
	StatusFailed: true,
}

type Item struct {
	ID                string  `json:"id"`
	SourceType        string  `json:"source_type"`
	SourcePath        string  `json:"source_path"`
	SourceFingerprint string  `json:"source_fingerprint"`
	Path              string  `json:"path"`
	InnerPath         *string `json:"inner_path,omitempty"`
	Size              int64   `json:"size"`
	MTimeNS           *int64  `json:"mtime_ns,omitempty"`
	CRC               *uint32 `json:"crc,omitempty"`
	Fingerprint       string  `json:"fingerprint"`
	Status            string  `json:"status"`
	EnqueuedAt        string  `json:"enqueued_at"`
	UpdatedAt         string  `json:"updated_at"`
	Error             *string `json:"error,omitempty"`
}

type Queue struct {
	path             string
	mu               sync.Mutex
	items            map[string]*Item
	fingerprintIndex map[string]string
	sourceIndex      map[string]struct{}
	appendCh         chan *Item
	closeCh          chan struct{}
}

func New(path string) (*Queue, error) {
	q := &Queue{
		path:             path,
		items:            map[string]*Item{},
		fingerprintIndex: map[string]string{},
		sourceIndex:      map[string]struct{}{},
		appendCh:         make(chan *Item, 4096),
		closeCh:          make(chan struct{}),
	}
	if err := q.load(); err != nil {
		return nil, err
	}
	go q.writerLoop()
	return q, nil
}

func buildID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func BuildFingerprint(sourceType, path string, innerPath *string, size int64, mtimeNS *int64, crc *uint32) string {
	parts := []string{sourceType, path, strconv64(size)}
	if innerPath != nil && *innerPath != "" {
		parts = append(parts, *innerPath)
	}
	if mtimeNS != nil {
		parts = append(parts, strconv64(*mtimeNS))
	}
	if crc != nil {
		parts = append(parts, strconv64(int64(*crc)))
	}
	return strings.Join(parts, "|")
}

func BuildSourceFingerprint(path string, size int64, mtimeNS *int64) string {
	parts := []string{path, strconv64(size)}
	if mtimeNS != nil {
		parts = append(parts, strconv64(*mtimeNS))
	}
	return strings.Join(parts, "|")
}

func strconv64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func (q *Queue) load() error {
	file, err := os.Open(q.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 1024*1024)
	scanner.Buffer(buffer, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item Item
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item.ID == "" {
			continue
		}
		q.items[item.ID] = &item
	}
	q.rebuildIndexes()
	return scanner.Err()
}

func (q *Queue) rebuildIndexes() {
	q.fingerprintIndex = map[string]string{}
	q.sourceIndex = map[string]struct{}{}
	for id, item := range q.items {
		q.fingerprintIndex[item.Fingerprint] = id
		key := item.SourceType + ":" + item.SourceFingerprint
		q.sourceIndex[key] = struct{}{}
	}
}

func (q *Queue) writerLoop() {
	file, err := os.OpenFile(q.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	batch := []*Item{}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		for _, item := range batch {
			data, err := json.Marshal(item)
			if err != nil {
				continue
			}
			writer.Write(data)
			writer.WriteByte('\n')
		}
		writer.Flush()
		batch = batch[:0]
	}

	for {
		select {
		case item := <-q.appendCh:
			batch = append(batch, item)
			if len(batch) >= 128 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-q.closeCh:
			flush()
			return
		}
	}
}

func (q *Queue) Close() {
	close(q.closeCh)
}

func (q *Queue) HasFingerprint(fingerprint string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.fingerprintIndex[fingerprint]
	return ok
}

func (q *Queue) HasSourceFingerprint(sourceType, sourceFingerprint string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.sourceIndex[sourceType+":"+sourceFingerprint]
	return ok
}

func (q *Queue) Enqueue(item Item) (*Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if item.Fingerprint == "" {
		return nil, errors.New("missing fingerprint")
	}
	if _, exists := q.fingerprintIndex[item.Fingerprint]; exists {
		return nil, nil
	}

	id, err := buildID()
	if err != nil {
		return nil, err
	}
	now := nowUTC()
	item.ID = id
	item.Status = StatusQueued
	item.EnqueuedAt = now
	item.UpdatedAt = now

	q.items[item.ID] = &item
	q.fingerprintIndex[item.Fingerprint] = item.ID
	q.sourceIndex[item.SourceType+":"+item.SourceFingerprint] = struct{}{}

	q.appendCh <- &item
	return &item, nil
}

func (q *Queue) UpdateStatus(id string, status string, errMsg *string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.items[id]
	if !ok {
		return errors.New("queue item not found")
	}
	item.Status = status
	item.UpdatedAt = nowUTC()
	item.Error = errMsg
	q.appendCh <- item
	return nil
}

func (q *Queue) Pending(limit int) []*Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	pending := []*Item{}
	for _, item := range q.items {
		if pendingStatuses[item.Status] {
			pending = append(pending, item)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].EnqueuedAt < pending[j].EnqueuedAt
	})
	if limit > 0 && len(pending) > limit {
		return pending[:limit]
	}
	return pending
}

func (q *Queue) Stats() map[string]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	counts := map[string]int{
		StatusQueued:  0,
		StatusSending: 0,
		StatusSent:    0,
		StatusFailed:  0,
	}
	for _, item := range q.items {
		if _, ok := counts[item.Status]; ok {
			counts[item.Status]++
		}
	}
	return counts
}
