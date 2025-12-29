package queue

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

	MetaType    = "queue_meta"
	MetaVersion = 1
)

var pendingStatuses = map[string]bool{
	StatusQueued: true,
	StatusFailed: true,
}

type Meta struct {
	Type    string     `json:"type"`
	Version int        `json:"version"`
	Params  MetaParams `json:"params"`
}

type WatchDirs []string

func (w *WatchDirs) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*w = nil
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			*w = nil
			return nil
		}
		*w = WatchDirs{value}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	*w = WatchDirs(cleaned)
	return nil
}

type MetaParams struct {
	Command   string    `json:"command"`
	WatchDir  WatchDirs `json:"watch_dir"`
	Recursive bool      `json:"recursive"`
	ChatID    string    `json:"chat_id"`
	TopicID   *int      `json:"topic_id,omitempty"`
	WithImage bool      `json:"with_image"`
	WithVideo bool      `json:"with_video"`
	WithAudio bool      `json:"with_audio"`
	WithAll   bool      `json:"with_all"`
	Include   []string  `json:"include,omitempty"`
	Exclude   []string  `json:"exclude,omitempty"`
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
	SendType          string  `json:"send_type,omitempty"`
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
	meta             *Meta
	metaChecked      bool
	metaFound        bool
}

func New(path string, meta *Meta) (*Queue, error) {
	q := &Queue{
		path:             path,
		items:            map[string]*Item{},
		fingerprintIndex: map[string]string{},
		sourceIndex:      map[string]struct{}{},
		appendCh:         make(chan *Item, 4096),
		closeCh:          make(chan struct{}),
		meta:             normalizeMeta(meta),
	}
	if err := q.load(); err != nil {
		return nil, err
	}
	if q.meta != nil && !q.metaChecked {
		if err := q.writeMeta(); err != nil {
			return nil, err
		}
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

func normalizeMeta(meta *Meta) *Meta {
	if meta == nil {
		return nil
	}
	copyMeta := *meta
	if copyMeta.Type == "" {
		copyMeta.Type = MetaType
	}
	if copyMeta.Version == 0 {
		copyMeta.Version = MetaVersion
	}
	copyMeta.Params.WatchDir = normalizeWatchDirs(meta.Params.WatchDir)
	copyMeta.Params.Include = append([]string{}, meta.Params.Include...)
	copyMeta.Params.Exclude = append([]string{}, meta.Params.Exclude...)
	sort.Strings(copyMeta.Params.Include)
	sort.Strings(copyMeta.Params.Exclude)
	return &copyMeta
}

func normalizeWatchDirs(dirs WatchDirs) WatchDirs {
	if len(dirs) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir != "" {
			cleaned = append(cleaned, dir)
		}
	}
	sort.Strings(cleaned)
	if len(cleaned) == 0 {
		return nil
	}
	return WatchDirs(cleaned)
}

func parseMeta(line string) (*Meta, bool, error) {
	var meta Meta
	if err := json.Unmarshal([]byte(line), &meta); err != nil {
		return nil, false, err
	}
	if meta.Type != MetaType || meta.Version != MetaVersion {
		return nil, false, nil
	}
	return normalizeMeta(&meta), true, nil
}

func metaMatches(expected *Meta, actual *Meta) bool {
	if expected == nil || actual == nil {
		return false
	}
	return reflect.DeepEqual(normalizeMeta(expected), normalizeMeta(actual))
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
		if !q.metaChecked {
			q.metaChecked = true
			meta, ok, err := parseMeta(line)
			if err != nil {
				if q.meta != nil {
					return errors.New("queue metadata missing or invalid. Use a different --queue-file")
				}
				continue
			}
			if ok {
				q.metaFound = true
				if q.meta != nil && !metaMatches(q.meta, meta) {
					return errors.New("queue metadata does not match current run parameters")
				}
				continue
			}
			if q.meta != nil {
				return errors.New("queue metadata missing. Use a different --queue-file")
			}
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

func (q *Queue) writeMeta() error {
	if q.meta == nil {
		return nil
	}
	q.meta = normalizeMeta(q.meta)
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(q.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(q.meta)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	q.metaChecked = true
	q.metaFound = true
	return nil
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
