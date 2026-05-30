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
	TypicalP    float32 // 0 = disabled
	Seed        uint32

	// Repetition penalties (0 = disabled).
	RepeatPenalty    float32 // > 1.0 penalises repeated tokens; 1.0 = off
	RepeatLastN      int32   // context window for repeat penalty; -1 = full context
	FrequencyPenalty float32 // additive frequency penalty
	PresencePenalty  float32 // additive presence penalty

	// Mirostat (mutually exclusive with TopK/TopP; Mirostat > 0 enables).
	Mirostat    int32   // 0=off, 1=Mirostat v1, 2=Mirostat v2
	MirostatTau float32 // target entropy (default 5.0)
	MirostatEta float32 // learning rate (default 0.1)
}

// DefaultSamplerOptions returns sensible defaults.
func DefaultSamplerOptions() SamplerOptions {
	return SamplerOptions{
		Temperature: 0.8,
		TopK:        40,
		TopP:        0.9,
		MinP:        0.05,
		Seed:        0,
		MirostatTau: 5.0,
		MirostatEta: 0.1,
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
// If grammarStr is non-empty, a grammar sampler is inserted BEFORE Mirostat
// (or before Dist in the standard path) — the canonical llama.cpp order.
//
// Sampler chain layout (standard):
//
//	[Penalties] → TopK → TopP | TypicalP → MinP → TempExt → [Grammar] → Dist
//
// Sampler chain layout (Mirostat):
//
//	[Penalties] → [Grammar] → Mirostat(v1|v2)
//
// Returns an error if the grammar string is set but llama.cpp rejects it.
func buildSampler(opts SamplerOptions, vocab llama.Vocab, grammarStr string) (llama.Sampler, error) {
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())

	// Repetition / frequency / presence penalties.
	// llama.cpp: repeat_penalty=1.0 means disabled; 0.0 causes degenerate output.
	if opts.RepeatPenalty > 0 || opts.FrequencyPenalty != 0 || opts.PresencePenalty != 0 {
		lastN := opts.RepeatLastN
		if lastN == 0 {
			lastN = 64 // llama.cpp default
		}
		repeatP := opts.RepeatPenalty
		if repeatP == 0 {
			repeatP = 1.0 // 0.0 would corrupt output; 1.0 = disabled
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitPenalties(lastN, repeatP, opts.FrequencyPenalty, opts.PresencePenalty))
	}

	if opts.Mirostat > 0 {
		tau := opts.MirostatTau
		if tau == 0 {
			tau = 5.0
		}
		eta := opts.MirostatEta
		if eta == 0 {
			eta = 0.1
		}
		// Grammar before Mirostat: grammar constrains the candidate set,
		// then Mirostat selects from the constrained distribution.
		if grammarStr != "" {
			grammarSampler := llama.SamplerInitGrammar(vocab, grammarStr, "root")
			if grammarSampler == 0 {
				llama.SamplerFree(chain)
				return 0, fmt.Errorf("engine: failed to initialise grammar sampler (invalid grammar string?)")
			}
			llama.SamplerChainAdd(chain, grammarSampler)
		}
		switch opts.Mirostat {
		case 1:
			// Mirostat v1 needs vocab size — pass 0 to let llama.cpp use the model's vocab size.
			llama.SamplerChainAdd(chain, llama.SamplerInitMirostat(0, opts.Seed, tau, eta, 100))
		default: // 2
			llama.SamplerChainAdd(chain, llama.SamplerInitMirostatV2(opts.Seed, tau, eta))
		}
	} else {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopK(opts.TopK))
		// TypicalP == 1.0 means "all tokens typical" (disabled) — fall through to TopP.
		if opts.TypicalP > 0 && opts.TypicalP < 1.0 {
			llama.SamplerChainAdd(chain, llama.SamplerInitTypical(opts.TypicalP, 1))
		} else {
			llama.SamplerChainAdd(chain, llama.SamplerInitTopP(opts.TopP, 1))
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitMinP(opts.MinP, 1))
		llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(opts.Temperature, 1.0, 1.0))
		if grammarStr != "" {
			grammarSampler := llama.SamplerInitGrammar(vocab, grammarStr, "root")
			if grammarSampler == 0 {
				llama.SamplerFree(chain)
				return 0, fmt.Errorf("engine: failed to initialise grammar sampler (invalid grammar string?)")
			}
			llama.SamplerChainAdd(chain, grammarSampler)
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitDist(opts.Seed))
	}
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
	// the output tokens to valid productions of the grammar (used by
	// structured output via the format field).
	GrammarStr string
	// Grammar is a GBNF grammar string for tool calling. When set, an
	// additional grammar sampler is built per-request with lazy trigger support.
	Grammar string
	// GrammarLazy, when non-empty, activates lazy-trigger mode: the grammar
	// only kicks in once one of these patterns is sampled (e.g. "\{").
	GrammarLazy []string
	// Messages, when non-nil, are applied via the model's native chat template
	// (ApplyChatTemplate) instead of the pre-built prompt string. This allows
	// thinking models (Qwen3, DeepSeek-R1) to receive their native template
	// including the enable_thinking control variable.
	Messages []ChatMessage
}

// ChatMessage is a simple role/content pair for use with the native chat template.
type ChatMessage struct {
	Role    string
	Content string
}

// Generate tokenises prompt and streams generated text pieces to out.
// It respects ctx cancellation between decode calls.
//
// Grammar priority:
//   - opts.Grammar + opts.GrammarLazy → lazy-trigger per-request grammar sampler
//     (used by tool calling)
//   - opts.GrammarStr → per-request grammar sampler without lazy trigger
//     (used by structured output via the format field)
//
// The per-request sampler is freed after generation; the session sampler is
// left unchanged.
func (s *Session) Generate(ctx context.Context, prompt string, opts GenerateOptions, out func(piece string) error) error {
	// If Messages are provided, build the prompt using the model's native chat
	// template (which supports thinking-model control variables like enable_thinking).
	// This is preferred over the pre-built chatml prompt for structured output.
	if len(opts.Messages) > 0 {
		var err error
		llamaMsgs := make([]llama.ChatMessage, len(opts.Messages))
		for i, m := range opts.Messages {
			llamaMsgs[i] = llama.NewChatMessage(m.Role, m.Content)
		}
		prompt, err = s.ApplyChatTemplate(llamaMsgs)
		if err != nil {
			return fmt.Errorf("engine: apply chat template: %w", err)
		}
	}

	tokens := llama.Tokenize(s.model.vocab, prompt, true, false)
	batch := llama.BatchGetOne(tokens)

	nPredict := opts.MaxTokens
	if nPredict == 0 {
		nPredict = 512 // 0 = use default; -1 = unlimited (passed as-is to decode loop)
	}

	// Build a per-request sampler when a grammar constraint is requested.
	// Tool calling uses Grammar+GrammarLazy (lazy trigger).
	// Structured output uses GrammarStr (eager).
	// Both avoid mutating the shared session sampler.
	activeSampler := s.sampler
	var requestSampler llama.Sampler
	switch {
	case opts.Grammar != "":
		var err error
		requestSampler, err = buildSamplerWithGrammar(opts.Sampler, s.model.vocab, opts.Grammar, opts.GrammarLazy)
		if err != nil {
			return fmt.Errorf("engine: %w", err)
		}
		activeSampler = requestSampler
	case opts.GrammarStr != "":
		var err error
		requestSampler, err = buildSampler(opts.Sampler, s.model.vocab, opts.GrammarStr)
		if err != nil {
			return err
		}
		activeSampler = requestSampler
	}
	defer func() {
		if requestSampler != 0 {
			llama.SamplerFree(requestSampler)
		}
	}()

	// Note: eager-grammar prefilling (feeding prompt tokens to SamplerAccept
	// before the generation loop) was removed. Structured output now uses lazy
	// grammars (Grammar + GrammarLazy) that activate only when the trigger
	// pattern "{" is sampled, so no prefill is needed. The GrammarStr path is
	// retained for future use but is not exercised by any current handler.

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

		token, sampleErr := safeSamplerSample(activeSampler, s.ctx)
		if sampleErr != nil {
			return fmt.Errorf("engine: grammar rejected all tokens (model likely generating non-JSON): %w", sampleErr)
		}
		if acceptErr := safeSamplerAccept(activeSampler, token); acceptErr != nil {
			return fmt.Errorf("engine: grammar accept failed for token %d: %w", token, acceptErr)
		}

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

// buildSamplerWithGrammar constructs a full sampler chain including a
// grammar constraint with optional lazy-trigger patterns.
//
// Sampler chain layout (standard):
//
//	[Penalties] → TopK → TopP | TypicalP → MinP → TempExt → [Grammar] → Dist
//
// Sampler chain layout (Mirostat):
//
//	[Penalties] → [Grammar] → Mirostat(v1|v2)
//
// When lazyPatterns is non-empty, SamplerInitGrammarLazyPatterns is used
// so the grammar only activates once one of the trigger patterns is emitted.
// Returns an error if the grammar sampler cannot be initialised (returns 0).
func buildSamplerWithGrammar(opts SamplerOptions, vocab llama.Vocab, grammar string, lazyPatterns []string) (llama.Sampler, error) {
	chain := llama.SamplerChainInit(llama.SamplerChainDefaultParams())

	// Repetition / frequency / presence penalties.
	// llama.cpp: repeat_penalty=1.0 means disabled; 0.0 causes degenerate output.
	if opts.RepeatPenalty > 0 || opts.FrequencyPenalty != 0 || opts.PresencePenalty != 0 {
		lastN := opts.RepeatLastN
		if lastN == 0 {
			lastN = 64
		}
		repeatP := opts.RepeatPenalty
		if repeatP == 0 {
			repeatP = 1.0 // 0.0 would corrupt output; 1.0 = disabled
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitPenalties(lastN, repeatP, opts.FrequencyPenalty, opts.PresencePenalty))
	}

	// grammarSampler builds the grammar sampler (lazy or eager).
	buildGrammarSampler := func() (llama.Sampler, error) {
		if len(lazyPatterns) > 0 {
			gs := llama.SamplerInitGrammarLazyPatterns(vocab, grammar, "root", lazyPatterns, nil)
			if gs == 0 {
				return 0, fmt.Errorf("failed to initialise lazy grammar sampler")
			}
			return gs, nil
		}
		gs := llama.SamplerInitGrammar(vocab, grammar, "root")
		if gs == 0 {
			return 0, fmt.Errorf("failed to initialise grammar sampler")
		}
		return gs, nil
	}

	if opts.Mirostat > 0 {
		tau := opts.MirostatTau
		if tau == 0 {
			tau = 5.0
		}
		eta := opts.MirostatEta
		if eta == 0 {
			eta = 0.1
		}
		// Grammar before Mirostat: grammar constrains the candidate set,
		// then Mirostat selects from the constrained distribution.
		gs, err := buildGrammarSampler()
		if err != nil {
			llama.SamplerFree(chain)
			return 0, err
		}
		llama.SamplerChainAdd(chain, gs)
		switch opts.Mirostat {
		case 1:
			llama.SamplerChainAdd(chain, llama.SamplerInitMirostat(0, opts.Seed, tau, eta, 100))
		default:
			llama.SamplerChainAdd(chain, llama.SamplerInitMirostatV2(opts.Seed, tau, eta))
		}
	} else {
		llama.SamplerChainAdd(chain, llama.SamplerInitTopK(opts.TopK))
		// TypicalP == 1.0 means "all tokens typical" (disabled) — fall through to TopP.
		if opts.TypicalP > 0 && opts.TypicalP < 1.0 {
			llama.SamplerChainAdd(chain, llama.SamplerInitTypical(opts.TypicalP, 1))
		} else {
			llama.SamplerChainAdd(chain, llama.SamplerInitTopP(opts.TopP, 1))
		}
		llama.SamplerChainAdd(chain, llama.SamplerInitMinP(opts.MinP, 1))
		llama.SamplerChainAdd(chain, llama.SamplerInitTempExt(opts.Temperature, 1.0, 1.0))
		gs, err := buildGrammarSampler()
		if err != nil {
			llama.SamplerFree(chain)
			return 0, err
		}
		llama.SamplerChainAdd(chain, gs)
		llama.SamplerChainAdd(chain, llama.SamplerInitDist(opts.Seed))
	}
	return chain, nil
}

// ApplyChatTemplate applies the model's chat template to messages and returns the prompt string.
func (s *Session) ApplyChatTemplate(messages []llama.ChatMessage) (string, error) {
	buf := make([]byte, 32768)
	n := llama.ChatApplyTemplate(s.model.template, messages, true, buf)
	if n < 0 {
		return "", fmt.Errorf("engine: chat template apply failed (code %d)", n)
	}
	return string(buf[:n]), nil
}

// safeSamplerSample calls llama.SamplerSample and recovers from the panic that
// llama.cpp throws when the grammar constraint eliminates all token candidates
// ("Unexpected empty grammar stack"). Returns a proper Go error instead.
func safeSamplerSample(sampler llama.Sampler, ctx llama.Context) (tok llama.Token, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return llama.SamplerSample(sampler, ctx, -1), nil
}

// safeSamplerAccept calls llama.SamplerAccept and recovers from grammar-stack panics.
func safeSamplerAccept(sampler llama.Sampler, token llama.Token) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	llama.SamplerAccept(sampler, token)
	return nil
}
