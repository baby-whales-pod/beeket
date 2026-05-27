// Package engine wraps the Yzma/llama.cpp library for Beeket.
// All FFI interaction is confined to this package.
package engine

import (
	"context"
	"fmt"
	"log/slog"
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
	sampler := buildSampler(opts)
	return &Session{model: model, ctx: ctx, sampler: sampler}, nil
}

// buildSampler constructs a sampler chain from options.
func buildSampler(opts SamplerOptions) llama.Sampler {
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	llama.SamplerChainAdd(chain, llama.SamplerInitTopK(opts.TopK))
	llama.SamplerChainAdd(chain, llama.SamplerInitTopP(opts.TopP, 1))
	llama.SamplerChainAdd(chain, llama.SamplerInitMinP(opts.MinP, 1))
	llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(opts.Temperature, 1.0, 1.0))
	llama.SamplerChainAdd(chain, llama.SamplerInitDist(opts.Seed))
	return chain
}

// Free releases the session's FFI resources.
func (s *Session) Free() {
	llama.Free(s.ctx)     //nolint:errcheck
}

// -------------------------------------------------------------------
// Generation
// -------------------------------------------------------------------

// GenerateOptions controls a single generation call.
type GenerateOptions struct {
	MaxTokens   int
	StopStrings []string
	Sampler     SamplerOptions
}

// Generate tokenises prompt and streams generated text pieces to out.
// It respects ctx cancellation between decode calls.
func (s *Session) Generate(ctx context.Context, prompt string, opts GenerateOptions, out func(piece string) error) error {
	tokens := llama.Tokenize(s.model.vocab, prompt, true, false)
	batch := llama.BatchGetOne(tokens)

	nPredict := opts.MaxTokens
	if nPredict <= 0 {
		nPredict = 512
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

		token := llama.SamplerSample(s.sampler, s.ctx, -1)
		llama.SamplerAccept(s.sampler, token)

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

// -------------------------------------------------------------------
// Embeddings
// -------------------------------------------------------------------

// Embed returns the embedding vector for the given text.
func (s *Session) Embed(ctx context.Context, text string) ([]float32, error) {
	// Set embedding mode on context params — for v0.1 we use a simple approach:
	// tokenize, decode with pooling, read the embeddings from the last token position.
	tokens := llama.Tokenize(s.model.vocab, text, true, true)
	batch := llama.BatchGetOne(tokens)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if _, err := llama.Encode(s.ctx, batch); err != nil {
		// Fall back to Decode for models that don't have a separate encoder.
		if _, err2 := llama.Decode(s.ctx, batch); err2 != nil {
			return nil, fmt.Errorf("engine: embed decode: %w", err2)
		}
	}

	// For v0.1 return a placeholder — full pooling support requires
	// ContextParams.Pooling to be set to mean/cls before context creation.
	// This is wired up properly in the scheduler when creating embed sessions.
	return nil, fmt.Errorf("engine: embedding extraction requires a dedicated embed session; use Engine.NewEmbedSession")
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
