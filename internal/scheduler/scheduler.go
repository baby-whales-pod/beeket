// Package scheduler manages per-model worker goroutines and request queuing.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/baby-whales-pod/beeket/internal/metrics"
	"github.com/baby-whales-pod/beeket/internal/models"
)

const (
	queueDepth = 32
	loadGrace  = 200 * time.Millisecond
)

// Request represents a single inference request enqueued in a worker.
type Request struct {
	ctx    context.Context
	prompt string
	opts   engine.GenerateOptions
	out    func(piece string) error
	errCh  chan error
}

// Worker owns a loaded model and its inference context, serving requests sequentially.
type Worker struct {
	mu        sync.Mutex
	model     *engine.Model
	session   *engine.Session
	manifest  *models.Manifest
	reqCh     chan *Request
	quit      chan struct{}
	lastUsed  time.Time
	keepAlive time.Duration
}

// Scheduler manages all active workers and enforces max-loaded limits.
type Scheduler struct {
	mu          sync.Mutex
	eng         *engine.Engine
	mgr         *models.Manager
	workers     map[string]*Worker // key: "name:tag"
	maxLoaded   int
	keepAlive   time.Duration
	contextSize uint32
	numParallel int
	samplerOpts engine.SamplerOptions
}

// Config holds Scheduler configuration.
type Config struct {
	MaxLoaded   int
	KeepAlive   time.Duration
	ContextSize uint32
	NumParallel int
	SamplerOpts engine.SamplerOptions
}

// New creates a Scheduler.
func New(eng *engine.Engine, mgr *models.Manager, cfg Config) *Scheduler {
	s := &Scheduler{
		eng:         eng,
		mgr:         mgr,
		workers:     make(map[string]*Worker),
		maxLoaded:   cfg.MaxLoaded,
		keepAlive:   cfg.KeepAlive,
		contextSize: cfg.ContextSize,
		numParallel: cfg.NumParallel,
		samplerOpts: cfg.SamplerOpts,
	}
	go s.evictionLoop()
	return s
}

// Generate enqueues a generation request for the named model.
// It blocks until the request is accepted (queue not full).
func (s *Scheduler) Generate(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
	w, err := s.getOrLoadWorker(name, tag)
	if err != nil {
		return err
	}

	req := &Request{
		ctx:    ctx,
		prompt: prompt,
		opts:   opts,
		out:    out,
		errCh:  make(chan error, 1),
	}

	select {
	case w.reqCh <- req:
	default:
		return fmt.Errorf("scheduler: model %s:%s queue full, try later", name, tag)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-req.errCh:
		return err
	}
}

// getOrLoadWorker returns an existing worker or loads the model to create a new one.
func (s *Scheduler) getOrLoadWorker(name, tag string) (*Worker, error) {
	key := name + ":" + tag
	s.mu.Lock()
	if w, ok := s.workers[key]; ok {
		s.mu.Unlock()
		return w, nil
	}

	// Enforce max loaded by evicting LRU.
	for len(s.workers) >= s.maxLoaded {
		if !s.evictLRU() {
			break
		}
	}
	s.mu.Unlock()

	// Load the model (may take a moment).
	mf, err := s.mgr.Get(name, tag)
	if err != nil {
		return nil, fmt.Errorf("scheduler: model %s:%s not found: %w", name, tag, err)
	}
	blobPath := s.mgr.BlobPath(mf)

	loadStart := time.Now()
	m, err := s.eng.LoadModel(blobPath)
	if err != nil {
		return nil, fmt.Errorf("scheduler: load model: %w", err)
	}

	sess, err := s.eng.NewSession(m, s.contextSize, s.samplerOpts)
	if err != nil {
		m.Free()
		return nil, fmt.Errorf("scheduler: new session: %w", err)
	}
	loadDur := time.Since(loadStart)
	metrics.ModelLoadDuration.WithLabelValues(key).Observe(loadDur.Seconds())

	w := &Worker{
		model:     m,
		session:   sess,
		manifest:  mf,
		reqCh:     make(chan *Request, queueDepth),
		quit:      make(chan struct{}),
		lastUsed:  time.Now(),
		keepAlive: s.keepAlive,
	}

	s.mu.Lock()
	s.workers[key] = w
	s.mu.Unlock()

	metrics.ModelsLoaded.Set(float64(len(s.workers)))
	go w.run()
	slog.Info("scheduler: model loaded", "model", key, "load_dur", loadDur)
	return w, nil
}

// evictLRU removes the least-recently-used worker. Caller must hold s.mu.
func (s *Scheduler) evictLRU() bool {
	var oldest *Worker
	var oldestKey string
	for k, w := range s.workers {
		if oldest == nil || w.lastUsed.Before(oldest.lastUsed) {
			oldest = w
			oldestKey = k
		}
	}
	if oldest == nil {
		return false
	}
	delete(s.workers, oldestKey)
	metrics.ModelsLoaded.Set(float64(len(s.workers)))
	metrics.ModelEvictionsTotal.WithLabelValues("lru").Inc()
	go oldest.stop()
	slog.Info("scheduler: evicted model", "model", oldestKey)
	return true
}

// evictionLoop periodically evicts workers idle longer than keepAlive.
func (s *Scheduler) evictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, w := range s.workers {
			if now.Sub(w.lastUsed) > w.keepAlive {
				delete(s.workers, key)
				metrics.ModelsLoaded.Set(float64(len(s.workers)))
				metrics.ModelEvictionsTotal.WithLabelValues("idle").Inc()
				go w.stop()
				slog.Info("scheduler: idle eviction", "model", key)
			}
		}
		s.mu.Unlock()
	}
}

// LoadedModels returns a snapshot of currently loaded model names.
func (s *Scheduler) LoadedModels() []LoadedInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []LoadedInfo
	for key, w := range s.workers {
		out = append(out, LoadedInfo{
			Name:     key,
			Size:     w.manifest.Size,
			LastUsed: w.lastUsed,
		})
	}
	return out
}

// LoadedInfo describes a currently loaded model.
type LoadedInfo struct {
	Name     string
	Size     int64
	LastUsed time.Time
}

// run is the worker's main loop; processes requests one at a time.
func (w *Worker) run() {
	for {
		select {
		case <-w.quit:
			return
		case req := <-w.reqCh:
			w.mu.Lock()
			w.lastUsed = time.Now()
			w.mu.Unlock()

			err := w.session.Generate(req.ctx, req.prompt, req.opts, req.out)
			req.errCh <- err
		}
	}
}

// stop signals the worker to stop and frees its resources.
func (w *Worker) stop() {
	close(w.quit)
	w.session.Free()
	w.model.Free()
}
