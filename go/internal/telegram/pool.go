package telegram

import (
	"math/rand"
	"strings"
	"sync"
	"time"
)

type URLPool struct {
	mu     sync.Mutex
	urls   []string
	counts map[string]int
	rng    *rand.Rand
}

func NewURLPool(urls []string) *URLPool {
	normalized := []string{}
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url != "" {
			normalized = append(normalized, url)
		}
	}
	return &URLPool{
		urls:   normalized,
		counts: map[string]int{},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *URLPool) Get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.urls) == 0 {
		return ""
	}
	min := int(^uint(0) >> 1)
	candidates := []string{}
	for _, url := range p.urls {
		count := p.counts[url]
		if count < min {
			min = count
			candidates = []string{url}
		} else if count == min {
			candidates = append(candidates, url)
		}
	}
	return candidates[p.rng.Intn(len(candidates))]
}

func (p *URLPool) Increment(url string) {
	if url == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[url] = p.counts[url] + 1
}

func (p *URLPool) Remove(url string) {
	if url == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	filtered := []string{}
	for _, entry := range p.urls {
		if entry != url {
			filtered = append(filtered, entry)
		}
	}
	delete(p.counts, url)
	p.urls = filtered
}

type TokenPool struct {
	mu     sync.Mutex
	tokens []string
	counts map[string]int
	rng    *rand.Rand
}

func NewTokenPool(tokens []string) *TokenPool {
	normalized := []string{}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" {
			normalized = append(normalized, token)
		}
	}
	return &TokenPool{
		tokens: normalized,
		counts: map[string]int{},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *TokenPool) Get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) == 0 {
		return ""
	}
	min := int(^uint(0) >> 1)
	candidates := []string{}
	for _, token := range p.tokens {
		count := p.counts[token]
		if count < min {
			min = count
			candidates = []string{token}
		} else if count == min {
			candidates = append(candidates, token)
		}
	}
	return candidates[p.rng.Intn(len(candidates))]
}

func (p *TokenPool) Increment(token string) {
	if token == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counts[token] = p.counts[token] + 1
}

func (p *TokenPool) Remove(token string) {
	if token == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	filtered := []string{}
	for _, entry := range p.tokens {
		if entry != token {
			filtered = append(filtered, entry)
		}
	}
	delete(p.counts, token)
	p.tokens = filtered
}
