// Package engine wraps the Yzma/llama.cpp library for Beeket.
// All FFI interaction is confined to this package.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/hybridgroup/yzma/pkg/llama"
)

// Engine manages the lifecycle of the llama.cpp shared library.
// It must be initialised once per process via New() before use.
type Engine struct {
	mu      sync.Mutex
	libPath string
	ready   bool
}

// New loads and initialises the llama.cpp library from libPath.
// Pass an empty string to use the YZMA_LIB environment variable or
// the OS default search path.
func New(libPath string) (*Engine, error) {
	if err := llama.Load(libPath); err != nil {
		return nil, fmt.Errorf("engine: load llama library: %w", err)
	}
	llama.LogSet(llama.LogSilent())
	llama.Init()
	return &Engine{libPath: libPath, ready: true}, nil
}

// Close frees library resources. Must be called when the engine is no longer needed.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ready {
		llama.Close()
		e.ready = false
	}
}

// LibPath returns the loaded library path.
func (e *Engine) LibPath() string { return llama.LibPath() }

// -------------------------------------------------------------------
// Model
// -------------------------------------------------------------------

// Model is a loaded GGUF model handle.
type Model struct {
	handle   llama.Model
	vocab    llama.Vocab
	path     string
	template string
}

// LoadModel loads a GGUF model from path.
func (e *Engine) LoadModel(path string) (*Model, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	params := llama.ModelDefaultParams()
	m, err := llama.ModelLoadFromFile(path, params)
	if err != nil {
		return nil, fmt.Errorf("engine: load model %q: %w", path, err)
	}
	vocab := llama.ModelGetVocab(m)
	tmpl := llama.ModelChatTemplate(m, "")
	if tmpl == "" {
		tmpl = "chatml"
	}
	slog.Info("engine: model loaded", "path", path, "template", tmpl)
	return &Model{handle: m, vocab: vocab, path: path, template: tmpl}, nil
}

// Free releases the model's FFI resources.
func (m *Model) Free() {
	llama.ModelFree(m.handle) //nolint:errcheck
}

// ChatTemplate returns the model's detected chat template name.
func (m *Model) ChatTemplate() string { return m.template }

// -------------------------------------------------------------------
// Session
// -------------------------------------------------------------------

// Session represents one inference context tied to a Model.
type Session struct {
	model   *Model
	ctx     llama.Context
	sampler llama.Sampler
	pos     int32 // current position in the context
}

// SamplerOptions configures the token sampler.
type SamplerOptions struct {
	Temperature float32
	TopK        int32
	TopP        float32
	MinP        float32
	Seed        uint32
}

// DefaultSamplerOptions returns sensible defaults.
func DefaultSamplerOptions() SamplerOptions {
	return SamplerOptions{
		Temperature: 0.8,
		TopK:        40,
		TopP:        0.9,
		MinP:        0.05,
		Seed:        0,
	}
}

// NewSession creates an inference context for model with the given context size.
func (e *Engine) NewSession(model *Model, contextSize uint32, opts SamplerOptions) (*Session, error) {
	cp := llama.ContextDefaultParams()
	cp.NCtx = contextSize
	cp.NBatch = 512
	ctx, err := llama.InitFromModel(model.handle, cp)
	if err != nil {
		return nil, fmt.Errorf("engine: init context: %w", err)
	}
	sampler, err := buildSampler(opts, model.vocab, "")
	if err != nil {
		llama.Free(ctx) //nolint:errcheck
		return nil, err
	}
	return &Session{model: model, ctx: ctx, sampler: sampler}, nil
}

// buildSampler constructs a sampler chain from options.
// If grammarStr is non-empty, a grammar sampler is prepended to the chain.
// Returns an error if the grammar string is set but llama.cpp rejects it.
func buildSampler(opts SamplerOptions, vocab llama.Vocab, grammarStr string) (llama.Sampler, error) {
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	if grammarStr != "" {
		grammarSampler := llama.SamplerInitGrammar(vocab, grammarStr, "root")
		if grammarSampler == 0 {
			llama.SamplerFree(chain)
			return 0, fmt.Errorf("engine: failed to initialise grammar sampler (invalid grammar string?)")
		}
		llama.SamplerChainAdd(chain, grammarSampler)
	}
	llama.SamplerChainAdd(chain, llama.SamplerInitTopK(opts.TopK))
	llama.SamplerChainAdd(chain, llama.SamplerInitTopP(opts.TopP, 1))
	llama.SamplerChainAdd(chain, llama.SamplerInitMinP(opts.MinP, 1))
	llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(opts.Temperature, 1.0, 1.0))
	llama.SamplerChainAdd(chain, llama.SamplerInitDist(opts.Seed))
	return chain, nil
}

// Free releases the session's FFI resources.
func (s *Session) Free() {
	llama.Free(s.ctx) //nolint:errcheck
}

// -------------------------------------------------------------------
// Generation
// -------------------------------------------------------------------

// GenerateOptions controls a single generation call.
type GenerateOptions struct {
	MaxTokens   int
	StopStrings []string
	Sampler     SamplerOptions
	// GrammarStr, if non-empty, is a GBNF grammar string that constrains
	// the output tokens to valid productions of the grammar.
	GrammarStr string
}

// Generate tokenises prompt and streams generated text pieces to out.
// It respects ctx cancellation between decode calls.
// If opts.GrammarStr is set, a temporary per-request sampler with a grammar
// constraint is built and used (then freed), leaving the session sampler unchanged.
func (s *Session) Generate(ctx context.Context, prompt string, opts GenerateOptions, out func(piece string) error) error {
	tokens := llama.Tokenize(s.model.vocab, prompt, true, false)
	batch := llama.BatchGetOne(tokens)

	nPredict := opts.MaxTokens
	if nPredict <= 0 {
		nPredict = 512
	}

	// Build a per-request sampler if a grammar constraint is requested.
	// This avoids mutating the shared session sampler.
	sampler := s.sampler
	if opts.GrammarStr != "" {
		var err error
		sampler, err = buildSampler(opts.Sampler, s.model.vocab, opts.GrammarStr)
		if err != nil {
			return err
		}
		defer llama.SamplerFree(sampler)
	}

	var buf [256]byte
	var generated strings.Builder

	for pos := s.pos; ; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if _, err := llama.Decode(s.ctx, batch); err != nil {
			return fmt.Errorf("engine: decode: %w", err)
		}
		pos += batch.NTokens

		token := llama.SamplerSample(sampler, s.ctx, -1)
		llama.SamplerAccept(sampler, token)

		if llama.VocabIsEOG(s.model.vocab, token) {
			break
		}

		n := llama.TokenToPiece(s.model.vocab, token, buf[:], 0, true)
		if n < 0 {
			n = -n
		}
		piece := string(buf[:n])
		generated.WriteString(piece)

		if err := out(piece); err != nil {
			return err
		}

		// Check stop strings.
		full := generated.String()
		for _, stop := range opts.StopStrings {
			if strings.HasSuffix(full, stop) {
				return nil
			}
		}

		nPredict--
		if nPredict <= 0 {
			break
		}

		batch = llama.BatchGetOne([]llama.Token{token})
		s.pos = pos
	}
	return nil
}

// EmbedSession is a llama.Context created with Embeddings=1 and a
// non-None PoolingType. It must not be used for generation.
type EmbedSession struct {
	model   *Model
	ctx     llama.Context
	pooling llama.PoolingType
	nEmbd   int32
}

// NewEmbedSession creates a dedicated context for embedding extraction.
// The context has Embeddings=1 and PoolingType=Mean set so that a single
// Encode call produces a pooled sequence vector.
func (e *Engine) NewEmbedSession(model *Model, contextSize uint32) (*EmbedSession, error) {
	cp := llama.ContextDefaultParams()
	cp.NCtx = contextSize
	cp.NBatch = contextSize // embed entire sequence in one batch
	cp.NUbatch = contextSize
	cp.Embeddings = 1
	cp.PoolingType = llama.PoolingTypeMean // fallback; model default takes precedence
	ctx, err := llama.InitFromModel(model.handle, cp)
	if err != nil {
		return nil, fmt.Errorf("engine: init embed context: %w", err)
	}
	// Ensure the context is in embedding mode (belt-and-suspenders).
	llama.SetEmbeddings(ctx, true)
	return &EmbedSession{
		model:   model,
		ctx:     ctx,
		pooling: llama.GetPoolingType(ctx),
		nEmbd:   llama.ModelNEmbd(model.handle),
	}, nil
}

// Free releases the embed session's FFI resources.
func (s *EmbedSession) Free() {
	llama.Free(s.ctx) //nolint:errcheck
}

// NEmbd returns the embedding dimension.
func (s *EmbedSession) NEmbd() int32 { return s.nEmbd }

// Embed tokenises text, encodes it, reads the embedding vector, copies it
// out of FFI memory, and L2-normalises it. Returns the vector and the token
// count so callers can populate PromptEvalCount.
func (s *EmbedSession) Embed(ctx context.Context, text string) ([]float32, int, error) {
	if s.nEmbd <= 0 {
		return nil, 0, fmt.Errorf("engine: model has no embedding dimension")
	}

	tokens := llama.Tokenize(s.model.vocab, text, true, true)
	if len(tokens) == 0 {
		return nil, 0, fmt.Errorf("engine: embed: empty input after tokenisation")
	}

	// Respect context cancellation before submitting to the FFI.
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	default:
	}

	batch := llama.BatchGetOne(tokens)

	// Encoder-only models (BERT, nomic-embed-text) use Encode.
	// Decoder models with Embeddings=1 use Decode. Try Encode first,
	// logging the error before falling back so it's visible in debug logs.
	if _, err := llama.Encode(s.ctx, batch); err != nil {
		slog.Debug("embed: Encode failed, falling back to Decode", "err", err)
		if _, err2 := llama.Decode(s.ctx, batch); err2 != nil {
			return nil, len(tokens), fmt.Errorf("engine: embed: both Encode and Decode failed: %w", err2)
		}
	}

	var raw []float32
	if s.pooling != llama.PoolingTypeNone {
		// Pooled: one vector per sequence (seq_id 0 from BatchGetOne).
		v, err := llama.GetEmbeddingsSeq(s.ctx, 0, s.nEmbd)
		if err == nil {
			raw = v
		}
	}
	if raw == nil {
		// Fallback: take the last token's embedding (Ollama default).
		v, err := llama.GetEmbeddingsIth(s.ctx, int32(len(tokens))-1, s.nEmbd)
		if err == nil {
			raw = v
		}
	}
	if raw == nil {
		return nil, len(tokens), fmt.Errorf("engine: embed: no embedding returned (pooling=%v)", s.pooling)
	}

	// Copy out of FFI-owned memory before it is invalidated.
	out := make([]float32, len(raw))
	copy(out, raw)
	l2Normalize(out)
	return out, len(tokens), nil
}

// l2Normalize divides each element of v by the vector's L2 norm in-place.
// If the norm is zero the vector is left unchanged.
func l2Normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	norm := float32(1.0 / math.Sqrt(sum))
	for i := range v {
		v[i] *= norm
	}
}

// -------------------------------------------------------------------
// Chat template helper
// -------------------------------------------------------------------

// ApplyChatTemplate applies the model's chat template to messages and returns the prompt string.
func (s *Session) ApplyChatTemplate(messages []llama.ChatMessage) (string, error) {
	buf := make([]byte, 32768)
	n := llama.ChatApplyTemplate(s.model.template, messages, true, buf)
	if n < 0 {
		return "", fmt.Errorf("engine: chat template apply failed (code %d)", n)
	}
	return string(buf[:n]), nil
}
