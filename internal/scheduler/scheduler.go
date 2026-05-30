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

// LastUsed returns the worker's last-used timestamp under the worker's own mutex,
// preventing data races when eviction loops read it concurrently.
func (w *Worker) LastUsed() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastUsed
}

// Scheduler manages all active workers and enforces max-loaded limits.
//
// Lock ordering: mu MUST always be acquired before embedMu. Every site that
// acquires both locks must follow this order to avoid ABBA deadlocks.
type Scheduler struct {
	mu           sync.Mutex
	embedMu      sync.Mutex
	eng          *engine.Engine
	mgr          *models.Manager
	workers      map[string]*Worker      // key: "name:tag"
	embedWorkers map[string]*EmbedWorker // key: "name:tag#embed"
	maxLoaded    int
	keepAlive    time.Duration
	contextSize  uint32
	numParallel  int
	samplerOpts  engine.SamplerOptions
}

// Config holds Scheduler configuration.
type Config struct {
	// MaxLoaded is the maximum number of models (generate + embed) kept in memory.
	MaxLoaded int
	// KeepAlive is how long an idle model stays loaded before eviction.
	KeepAlive time.Duration
	// ContextSize is the per-session context window (in tokens).
	ContextSize uint32
	// NumParallel is reserved for future parallel-sequence support.
	NumParallel int
	SamplerOpts engine.SamplerOptions
}

// New creates a Scheduler.
func New(eng *engine.Engine, mgr *models.Manager, cfg Config) *Scheduler {
	s := &Scheduler{
		eng:          eng,
		mgr:          mgr,
		workers:      make(map[string]*Worker),
		embedWorkers: make(map[string]*EmbedWorker),
		maxLoaded:    cfg.MaxLoaded,
		keepAlive:    cfg.KeepAlive,
		contextSize:  cfg.ContextSize,
		numParallel:  cfg.NumParallel,
		samplerOpts:  cfg.SamplerOpts,
	}
	go s.evictionLoop()
	return s
}

// totalLoadedLocked returns the total number of loaded workers across both maps.
// Caller must hold BOTH mu and embedMu.
func (s *Scheduler) totalLoadedLocked() int {
	return len(s.workers) + len(s.embedWorkers)
}

// setModelsLoadedGaugeLocked updates the beeket_models_loaded gauge.
// Caller must hold BOTH mu and embedMu.
func (s *Scheduler) setModelsLoadedGaugeLocked() {
	metrics.ModelsLoaded.Set(float64(s.totalLoadedLocked()))
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
// Lock order: mu (only) for the fast-path; both mu+embedMu for gauge updates.
func (s *Scheduler) getOrLoadWorker(name, tag string) (*Worker, error) {
	key := name + ":" + tag

	s.mu.Lock()
	if w, ok := s.workers[key]; ok {
		s.mu.Unlock()
		return w, nil
	}
	// Enforce max loaded by evicting LRU (need both maps for total count).
	s.embedMu.Lock()
	for s.totalLoadedLocked() >= s.maxLoaded {
		if !s.evictLRULocked() && !s.evictLRUEmbedLocked() {
			break
		}
	}
	s.embedMu.Unlock()
	s.mu.Unlock()

	// Load the model outside the lock (may take a while).
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

	newWorker := &Worker{
		model:     m,
		session:   sess,
		manifest:  mf,
		reqCh:     make(chan *Request, queueDepth),
		quit:      make(chan struct{}),
		lastUsed:  time.Now(),
		keepAlive: s.keepAlive,
	}

	// TOCTOU double-check under both locks (mu before embedMu per ordering rule).
	s.mu.Lock()
	s.embedMu.Lock()
	if existing, ok := s.workers[key]; ok {
		s.embedMu.Unlock()
		s.mu.Unlock()
		newWorker.session.Free()
		newWorker.model.Free()
		return existing, nil
	}
	s.workers[key] = newWorker
	s.setModelsLoadedGaugeLocked()
	s.embedMu.Unlock()
	s.mu.Unlock()

	go newWorker.run()
	slog.Info("scheduler: model loaded", "model", key, "load_dur", loadDur)
	return newWorker, nil
}

// evictLRULocked removes the least-recently-used generate worker.
// Caller must hold BOTH mu and embedMu.
func (s *Scheduler) evictLRULocked() bool {
	var oldest *Worker
	var oldestKey string
	for k, w := range s.workers {
		if oldest == nil || w.LastUsed().Before(oldest.LastUsed()) {
			oldest = w
			oldestKey = k
		}
	}
	if oldest == nil {
		return false
	}
	delete(s.workers, oldestKey)
	s.setModelsLoadedGaugeLocked()
	metrics.ModelEvictionsTotal.WithLabelValues("lru").Inc()
	go oldest.stop()
	slog.Info("scheduler: evicted model", "model", oldestKey)
	return true
}

// evictionLoop periodically evicts workers idle longer than keepAlive.
// Acquires mu then embedMu (consistent lock order).
func (s *Scheduler) evictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		s.embedMu.Lock()

		for key, w := range s.workers {
			if now.Sub(w.LastUsed()) > w.keepAlive {
				delete(s.workers, key)
				s.setModelsLoadedGaugeLocked()
				metrics.ModelEvictionsTotal.WithLabelValues("idle").Inc()
				go w.stop()
				slog.Info("scheduler: idle eviction", "model", key)
			}
		}
		// B3: also sweep embed workers.
		for key, w := range s.embedWorkers {
			if now.Sub(w.LastUsed()) > w.keepAlive {
				delete(s.embedWorkers, key)
				s.setModelsLoadedGaugeLocked()
				metrics.ModelEvictionsTotal.WithLabelValues("idle").Inc()
				go w.stop()
				slog.Info("scheduler: embed idle eviction", "model", key)
			}
		}

		s.embedMu.Unlock()
		s.mu.Unlock()
	}
}

// LoadedModels returns a snapshot of currently loaded model names.
// Acquires mu then embedMu (consistent lock order).
func (s *Scheduler) LoadedModels() []LoadedInfo {
	s.mu.Lock()
	s.embedMu.Lock()
	defer s.embedMu.Unlock()
	defer s.mu.Unlock()

	var out []LoadedInfo
	for key, w := range s.workers {
		out = append(out, LoadedInfo{
			Name:     key,
			Size:     w.manifest.Size,
			LastUsed: w.LastUsed(),
		})
	}
	for key, w := range s.embedWorkers {
		out = append(out, LoadedInfo{
			Name:     key,
			Size:     w.manifest.Size,
			LastUsed: w.LastUsed(),
		})
	}
	return out
}

// LoadedInfo describes a currently loaded model in the scheduler.
type LoadedInfo struct {
	// Name is the "name:tag" key.
	Name     string
	Size     int64
	LastUsed time.Time
}

// -------------------------------------------------------------------
// Embedding worker
// -------------------------------------------------------------------

// EmbedWorker owns a dedicated EmbedSession for a model.
type EmbedWorker struct {
	mu        sync.Mutex
	session   *engine.EmbedSession
	model     *engine.Model
	manifest  *models.Manifest
	reqCh     chan *embedRequest
	quit      chan struct{}
	lastUsed  time.Time
	keepAlive time.Duration
}

type embedRequest struct {
	ctx  context.Context
	text string
	out  chan embedResult
}

type embedResult struct {
	vec     []float32
	nTokens int
	err     error
}

// LastUsed returns the embed worker's last-used timestamp under its mutex.
func (w *EmbedWorker) LastUsed() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastUsed
}

// Embed enqueues an embedding request and waits for the result.
func (s *Scheduler) Embed(ctx context.Context, name, tag, input string) ([]float32, int, error) {
	w, err := s.getOrLoadEmbedWorker(name, tag)
	if err != nil {
		return nil, 0, err
	}

	req := &embedRequest{
		ctx:  ctx,
		text: input,
		out:  make(chan embedResult, 1),
	}

	select {
	case w.reqCh <- req:
	default:
		return nil, 0, fmt.Errorf("scheduler: embed worker %s:%s queue full", name, tag)
	}

	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case res := <-req.out:
		return res.vec, res.nTokens, res.err
	}
}

// getOrLoadEmbedWorker returns the embed worker for name:tag, loading if needed.
// Lock order: mu before embedMu.
func (s *Scheduler) getOrLoadEmbedWorker(name, tag string) (*EmbedWorker, error) {
	key := name + ":" + tag + "#embed"

	s.mu.Lock()
	s.embedMu.Lock()
	if w, ok := s.embedWorkers[key]; ok {
		s.embedMu.Unlock()
		s.mu.Unlock()
		return w, nil
	}
	// Enforce max loaded across both maps.
	for s.totalLoadedLocked() >= s.maxLoaded {
		if !s.evictLRUEmbedLocked() && !s.evictLRULocked() {
			break
		}
	}
	s.embedMu.Unlock()
	s.mu.Unlock()

	// Load outside locks.
	mf, err := s.mgr.Get(name, tag)
	if err != nil {
		return nil, fmt.Errorf("scheduler: embed model %s:%s not found: %w", name, tag, err)
	}
	blobPath := s.mgr.BlobPath(mf)

	loadStart := time.Now()
	m, err := s.eng.LoadModel(blobPath)
	if err != nil {
		return nil, fmt.Errorf("scheduler: embed load model: %w", err)
	}
	esess, err := s.eng.NewEmbedSession(m, s.contextSize)
	if err != nil {
		m.Free()
		return nil, fmt.Errorf("scheduler: embed new session: %w", err)
	}
	loadDur := time.Since(loadStart)

	nw := &EmbedWorker{
		session:   esess,
		model:     m,
		manifest:  mf,
		reqCh:     make(chan *embedRequest, queueDepth),
		quit:      make(chan struct{}),
		lastUsed:  time.Now(),
		keepAlive: s.keepAlive,
	}

	// TOCTOU double-check under both locks (mu before embedMu).
	s.mu.Lock()
	s.embedMu.Lock()
	if existing, ok := s.embedWorkers[key]; ok {
		s.embedMu.Unlock()
		s.mu.Unlock()
		esess.Free()
		m.Free()
		return existing, nil
	}
	s.embedWorkers[key] = nw
	s.setModelsLoadedGaugeLocked()
	metrics.ModelLoadDuration.WithLabelValues(key).Observe(loadDur.Seconds())
	s.embedMu.Unlock()
	s.mu.Unlock()

	go nw.run()
	slog.Info("scheduler: embed worker loaded", "model", key, "load_dur", loadDur)
	return nw, nil
}

// evictLRUEmbedLocked removes the least-recently-used embed worker.
// Caller must hold BOTH mu and embedMu.
func (s *Scheduler) evictLRUEmbedLocked() bool {
	var oldest *EmbedWorker
	var oldestKey string
	for k, w := range s.embedWorkers {
		if oldest == nil || w.LastUsed().Before(oldest.LastUsed()) {
			oldest = w
			oldestKey = k
		}
	}
	if oldest == nil {
		return false
	}
	delete(s.embedWorkers, oldestKey)
	s.setModelsLoadedGaugeLocked()
	metrics.ModelEvictionsTotal.WithLabelValues("lru").Inc()
	go oldest.stop()
	slog.Info("scheduler: embed worker evicted", "model", oldestKey)
	return true
}

func (w *EmbedWorker) run() {
	for {
		select {
		case <-w.quit:
			return
		case req := <-w.reqCh:
			w.mu.Lock()
			w.lastUsed = time.Now()
			w.mu.Unlock()
			vec, n, err := w.session.Embed(req.ctx, req.text)
			req.out <- embedResult{vec: vec, nTokens: n, err: err}
		}
	}
}

func (w *EmbedWorker) stop() {
	close(w.quit)
	w.session.Free()
	w.model.Free()
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
